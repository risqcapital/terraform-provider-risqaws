// Copyright (c) Risq Research Ltd.
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &SsmCommandResource{}

var (
	initialRetryBackoff = time.Second
	maxRetryBackoff     = 30 * time.Second
	backoffSettingsMu   sync.Mutex
)

type ssmAPI interface {
	SendCommand(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error)
	ListCommandInvocations(ctx context.Context, params *ssm.ListCommandInvocationsInput, optFns ...func(*ssm.Options)) (*ssm.ListCommandInvocationsOutput, error)
	DescribeInstanceInformation(ctx context.Context, params *ssm.DescribeInstanceInformationInput, optFns ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error)
}

func NewSsmCommandResource() resource.Resource {
	return &SsmCommandResource{}
}

// SsmCommandResource defines the resource implementation.
type SsmCommandResource struct {
	ssm ssmAPI
}

// SsmCommandResourceModel describes the resource data model.
type SsmCommandResourceModel struct {
	Id              types.String          `tfsdk:"id"`
	DocumentName    types.String          `tfsdk:"document_name"`
	DocumentVersion types.String          `tfsdk:"document_version"`
	Targets         []TargetResourceModel `tfsdk:"targets"`
	Parameters      types.Map             `tfsdk:"parameters"`
	Timeouts        timeouts.Value        `tfsdk:"timeouts"`
}

// TargetResourceModel describes the target block in the resource.
type TargetResourceModel struct {
	Key    types.String   `tfsdk:"key"`
	Values []types.String `tfsdk:"values"`
}

func (r *SsmCommandResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssm_command"
}

func (r *SsmCommandResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "SSM Command Resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"document_name": schema.StringAttribute{
				MarkdownDescription: "The name of the SSM document to use.",
				Required:            true,
			},
			"document_version": schema.StringAttribute{
				MarkdownDescription: "The version of the SSM document to use.",
				Optional:            true,
			},
			"parameters": schema.MapAttribute{
				MarkdownDescription: "The parameters to pass to the SSM document.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
			}),
		},

		Blocks: map[string]schema.Block{
			"targets": schema.ListNestedBlock{
				MarkdownDescription: "The list of targets to send the command to.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "The key of the target.",
							Required:            true,
						},
						"values": schema.ListAttribute{
							MarkdownDescription: "The values of the target.",
							Required:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
		},
	}
}

func (r *SsmCommandResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	config, ok := req.ProviderData.(aws.Config)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *aws.Config, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	// Initialize the ssm client
	r.ssm = ssm.NewFromConfig(config)
}

func (r *SsmCommandResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data SsmCommandResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	targets := make([]ssmtypes.Target, len(data.Targets))
	for i, target := range data.Targets {
		targets[i] = ssmtypes.Target{
			Key:    target.Key.ValueStringPointer(),
			Values: make([]string, len(target.Values)),
		}
		for j, value := range target.Values {
			targets[i].Values[j] = value.ValueString()
		}
	}

	parameters := make(map[string][]string)
	{
		unparsed := make(map[string]string)
		err := data.Parameters.ElementsAs(ctx, &unparsed, false)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to convert unparsed, got error: %s", err))
			return
		}
		for k, v := range unparsed {
			parameters[k] = []string{v}
		}
	}

	createTimeout, createTimeoutErr := data.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(createTimeoutErr...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	targetInstanceIDs := collectTargetInstanceIDs(targets)
	readinessDiag := waitForSSMInstanceReadiness(ctx, r.ssm, targetInstanceIDs)
	resp.Diagnostics.Append(readinessDiag...)
	if resp.Diagnostics.HasError() {
		return
	}

	command, sendCommandDiag := sendCommandWithRetry(ctx, r.ssm, &ssm.SendCommandInput{
		DocumentName:    data.DocumentName.ValueStringPointer(),
		DocumentVersion: data.DocumentVersion.ValueStringPointer(),
		Targets:         targets,
		Parameters:      parameters,
	}, data.DocumentName.ValueString(), targetInstanceIDs)
	resp.Diagnostics.Append(sendCommandDiag...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Id = types.StringValue(*command.Command.CommandId)

	backoff := time.Second
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			resp.Diagnostics.AddError("Operation Cancelled", fmt.Sprintf("Context cancelled before attempt %d: %s", attempt+1, ctx.Err()))
			return
		}

		// Diagnostics with Severity warnings are treated as retriable errors
		attemptDiag := PollCommandInvocation(ctx, r.ssm, command)

		if attemptDiag.HasError() {
			resp.Diagnostics.Append(attemptDiag...)
			return
		} else if attemptDiag.WarningsCount() > 0 {
			tflog.Warn(ctx, fmt.Sprintf("Attempt %d failed with retriable error: %s. Retrying in %s...", attempt+1, attemptDiag, backoff))
			select {
			case <-time.After(backoff):
				// Exponential backoff with a maximum of 30 seconds
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
				continue
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					resp.Diagnostics.AddError("Timeout while waiting for SSM Command to complete", fmt.Sprintf("Timeout occurred while waiting on command %s (polled %d times over %s)", *command.Command.CommandId, attempt+1, createTimeout))
					return
				} else {
					resp.Diagnostics.AddError("Operation Cancelled", fmt.Sprintf("Context cancelled before attempt %d: %s", attempt+1, ctx.Err()))
					return
				}
			}
		} else {
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}
	}
}

func sendCommandWithRetry(ctx context.Context, client ssmAPI, input *ssm.SendCommandInput, documentName string, targetInstanceIDs []string) (*ssm.SendCommandOutput, diag.Diagnostics) {
	diagnostics := diag.Diagnostics{}
	backoff := initialRetryBackoffValue()
	if backoff <= 0 {
		backoff = time.Second
	}

	for attempt := 0; ; attempt++ {
		command, err := client.SendCommand(ctx, input)
		if err == nil {
			return command, diagnostics
		}

		if !isRetriableSendCommandReadinessError(err) {
			diagnostics.AddError("Client Error", fmt.Sprintf("Unable to send command, got error: %s", err))
			return nil, diagnostics
		}

		targetsLogValue := "none"
		if len(targetInstanceIDs) > 0 {
			targetsLogValue = strings.Join(targetInstanceIDs, ", ")
		}
		tflog.Warn(ctx, fmt.Sprintf("SendCommand attempt %d failed due to target readiness (%s). Retrying in %s. document=%q targets=%s", attempt+1, err, backoff, documentName, targetsLogValue))

		select {
		case <-time.After(backoff):
			backoff = nextBackoff(backoff)
		case <-ctx.Done():
			diagnostics.AddError(
				"SendCommand target readiness retries exhausted",
				fmt.Sprintf("Timed out retrying SendCommand due to target readiness for document %q and targets [%s]: %s", documentName, strings.Join(targetInstanceIDs, ", "), ctx.Err()),
			)
			return nil, diagnostics
		}
	}
}

func waitForSSMInstanceReadiness(ctx context.Context, client ssmAPI, instanceIDs []string) diag.Diagnostics {
	diagnostics := diag.Diagnostics{}
	if len(instanceIDs) == 0 {
		return diagnostics
	}

	backoff := initialRetryBackoffValue()
	if backoff <= 0 {
		backoff = time.Second
	}

	for attempt := 0; ; attempt++ {
		statusByInstance, err := describeInstancePingStatuses(ctx, client, instanceIDs)
		if err != nil {
			diagnostics.AddError("Readiness Poll AWS Error", fmt.Sprintf("Unable to DescribeInstanceInformation while waiting for SSM target readiness: %s", err))
			return diagnostics
		}

		notReady := unresolvedReadinessStatuses(instanceIDs, statusByInstance)
		if len(notReady) == 0 {
			return diagnostics
		}

		tflog.Warn(ctx, fmt.Sprintf("SSM target readiness attempt %d still waiting on [%s]. Retrying in %s", attempt+1, strings.Join(notReady, ", "), backoff))
		select {
		case <-time.After(backoff):
			backoff = nextBackoff(backoff)
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				diagnostics.AddError(
					"Readiness Timeout",
					fmt.Sprintf("Timed out waiting for target instances to become SSM managed and online: [%s]", strings.Join(notReady, ", ")),
				)
			} else {
				diagnostics.AddError(
					"Operation Cancelled",
					fmt.Sprintf("Context cancelled while waiting for SSM target instance readiness: %s", ctx.Err()),
				)
			}
			return diagnostics
		}
	}
}

func describeInstancePingStatuses(ctx context.Context, client ssmAPI, instanceIDs []string) (map[string]ssmtypes.PingStatus, error) {
	const maxFilterValues = 50
	statusByInstance := map[string]ssmtypes.PingStatus{}

	for start := 0; start < len(instanceIDs); start += maxFilterValues {
		end := start + maxFilterValues
		if end > len(instanceIDs) {
			end = len(instanceIDs)
		}
		chunk := instanceIDs[start:end]

		output, err := client.DescribeInstanceInformation(ctx, &ssm.DescribeInstanceInformationInput{
			Filters: []ssmtypes.InstanceInformationStringFilter{
				{
					Key:    aws.String("InstanceIds"),
					Values: chunk,
				},
			},
		})
		if err != nil {
			return nil, err
		}

		for _, instanceInfo := range output.InstanceInformationList {
			if instanceInfo.InstanceId == nil {
				continue
			}

			statusByInstance[*instanceInfo.InstanceId] = instanceInfo.PingStatus
		}
	}

	return statusByInstance, nil
}

func unresolvedReadinessStatuses(targetInstanceIDs []string, statusByInstance map[string]ssmtypes.PingStatus) []string {
	notReady := make([]string, 0, len(targetInstanceIDs))
	for _, instanceID := range targetInstanceIDs {
		status, ok := statusByInstance[instanceID]
		if !ok {
			notReady = append(notReady, fmt.Sprintf("%s:not-managed", instanceID))
			continue
		}

		if status != ssmtypes.PingStatusOnline {
			notReady = append(notReady, fmt.Sprintf("%s:%s", instanceID, status))
		}
	}

	slices.Sort(notReady)
	return notReady
}

func collectTargetInstanceIDs(targets []ssmtypes.Target) []string {
	ids := map[string]struct{}{}
	for _, target := range targets {
		if target.Key == nil || !strings.EqualFold(*target.Key, "InstanceIds") {
			continue
		}

		for _, value := range target.Values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			ids[trimmed] = struct{}{}
		}
	}

	if len(ids) == 0 {
		return nil
	}

	instanceIDs := make([]string, 0, len(ids))
	for instanceID := range ids {
		instanceIDs = append(instanceIDs, instanceID)
	}
	slices.Sort(instanceIDs)
	return instanceIDs
}

func isRetriableSendCommandReadinessError(err error) bool {
	lowerMessage := strings.ToLower(err.Error())

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(apiErr.ErrorCode())
		message := strings.ToLower(apiErr.ErrorMessage())
		if code == "invalidinstanceid" && (containsReadinessMarker(message) || containsReadinessMarker(lowerMessage)) {
			return true
		}
	}

	return containsReadinessMarker(lowerMessage)
}

func containsReadinessMarker(message string) bool {
	markers := []string{
		"no instances in target list are managed instances",
		"not in valid state for account",
		"is not in valid state",
		"unable to find managed instance",
		"is not connected",
		"is not valid for account",
	}

	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

func initialRetryBackoffValue() time.Duration {
	backoffSettingsMu.Lock()
	defer backoffSettingsMu.Unlock()
	return initialRetryBackoff
}

func nextBackoff(current time.Duration) time.Duration {
	backoffSettingsMu.Lock()
	defer backoffSettingsMu.Unlock()

	next := current * 2
	if next > maxRetryBackoff {
		return maxRetryBackoff
	}
	return next
}

func PollCommandInvocation(ctx context.Context, client ssmAPI, command *ssm.SendCommandOutput) diag.Diagnostics {
	// Severity = Error is treated as a fatal
	// Severity = Warning is treated as a retriable error
	diagnostics := diag.Diagnostics{}

	listCommandInvocationsOutput, err := client.ListCommandInvocations(ctx, &ssm.ListCommandInvocationsInput{
		CommandId: command.Command.CommandId,
		Details:   true,
	})
	if err != nil {
		diagnostics.AddError("AWS Client Error", fmt.Sprintf("Unable to ListCommandInvocations, got error: %s", err))
		return diagnostics
	}

	invocationCount := int32(len(listCommandInvocationsOutput.CommandInvocations))
	if invocationCount == 0 {
		// Continue polling as the command invocation is not yet available and the api is eventually consistent
		diagnostics.AddWarning("Command Invocations Not Found", fmt.Sprintf("No command invocations found for command ID: %s", *command.Command.CommandId))
		return diagnostics
	}

	for _, invocation := range listCommandInvocationsOutput.CommandInvocations {
		if invocation.Status == ssmtypes.CommandInvocationStatusPending ||
			invocation.Status == ssmtypes.CommandInvocationStatusInProgress ||
			invocation.Status == ssmtypes.CommandInvocationStatusDelayed ||
			invocation.Status == ssmtypes.CommandInvocationStatusCancelling {
			// Continue polling if the command is still in progress
			diagnostics.AddWarning("Command Invocation In Progress", fmt.Sprintf("Command invocation is still in progress for command ID: %s, instance ID: %s", *command.Command.CommandId, *invocation.InstanceId))
			return diagnostics
		}

		for _, plugin := range invocation.CommandPlugins {
			if plugin.Status == ssmtypes.CommandPluginStatusSuccess {
				continue
			} else {
				diagnostics.AddError(
					fmt.Sprintf("%s to execute SSM Command %s / %s / %s", *plugin.StatusDetails, *command.Command.CommandId, *invocation.InstanceId, *plugin.Name),
					*plugin.Output,
				)
			}
		}

		if diagnostics.HasError() {
			return diagnostics
		}

		if invocation.Status == ssmtypes.CommandInvocationStatusFailed || invocation.Status == ssmtypes.CommandInvocationStatusTimedOut || invocation.Status == ssmtypes.CommandInvocationStatusCancelled {
			diagnostics.AddError("Client Error", fmt.Sprintf("Command invocation failed, got error: %s", *invocation.StatusDetails))
			return diagnostics
		}
	}

	return diagnostics
}

func (r *SsmCommandResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *SsmCommandResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unsupported Operation", "Update is not supported for SSM Command resource")
}

func (r *SsmCommandResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}
