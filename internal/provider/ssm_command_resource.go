// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	json2 "encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"time"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &SsmCommandResource{}

func NewSsmCommandResource() resource.Resource {
	return &SsmCommandResource{}
}

// SsmCommandResource defines the resource implementation.
type SsmCommandResource struct {
	ssm *ssm.Client
}

// SsmCommandResourceModel describes the resource data model.
type SsmCommandResourceModel struct {
	Id              types.String `tfsdk:"id"`
	DocumentName    types.String `tfsdk:"document_name"`
	DocumentVersion types.String `tfsdk:"document_version"`
	Targets         []Target     `tfsdk:"targets"`
	Parameters      types.Map    `tfsdk:"parameters"`
}

// Target describes the target block in the resource.
type Target struct {
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

	command, err := r.ssm.SendCommand(ctx, &ssm.SendCommandInput{
		DocumentName: data.DocumentName.ValueStringPointer(),
		Targets:      targets,
		Parameters:   parameters,
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to send command, got error: %s", err))
		return
	}

	data.Id = types.StringValue(*command.Command.CommandId)

OUTER:
	for {
		listCommandInvocationsOutput, err := r.ssm.ListCommandInvocations(ctx, &ssm.ListCommandInvocationsInput{
			CommandId: command.Command.CommandId,
			Details:   true,
		})
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to ListCommandInvocations, got error: %s", err))
			return
		}

		invocationCount := int32(len(listCommandInvocationsOutput.CommandInvocations))

		if invocationCount == 0 {
			// TODO: Is this the right way to sleep before retrying?
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, invocation := range listCommandInvocationsOutput.CommandInvocations {
			if invocation.Status == ssmtypes.CommandInvocationStatusPending ||
				invocation.Status == ssmtypes.CommandInvocationStatusInProgress ||
				invocation.Status == ssmtypes.CommandInvocationStatusDelayed ||
				invocation.Status == ssmtypes.CommandInvocationStatusCancelling {
				// TODO: Is this the right way to sleep before retrying?
				time.Sleep(500 * time.Millisecond)
				continue OUTER
			}

			for _, plugin := range invocation.CommandPlugins {
				if plugin.Status == ssmtypes.CommandPluginStatusSuccess {
					continue
				} else {
					resp.Diagnostics.AddError(
						fmt.Sprintf("%s to execute SSM Command %s / %s / %s", *plugin.StatusDetails, *command.Command.CommandId, *invocation.InstanceId, *plugin.Name),
						*plugin.Output,
					)
				}
			}

			if resp.Diagnostics.HasError() {
				continue
			}

			json, err := json2.Marshal(invocation)
			if err != nil {
				resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal command invocation, got error: %s", err))
				return
			}

			if invocation.Status == ssmtypes.CommandInvocationStatusFailed || invocation.Status == ssmtypes.CommandInvocationStatusTimedOut || invocation.Status == ssmtypes.CommandInvocationStatusCancelled {
				resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Command invocation failed, got error: %s", json))
				return
			}
		}

		break
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SsmCommandResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *SsmCommandResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unsupported Operation", "Update is not supported for SSM Command resource")
}

func (r *SsmCommandResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}
