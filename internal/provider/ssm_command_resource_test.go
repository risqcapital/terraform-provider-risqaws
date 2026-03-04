package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go"
)

func TestWaitForSSMInstanceReadinessImmediateOnline(t *testing.T) {
	setRetryBackoffForTest(t)

	client := &fakeSSMClient{
		describeOutputs: []*ssm.DescribeInstanceInformationOutput{
			{
				InstanceInformationList: []ssmtypes.InstanceInformation{
					{
						InstanceId: aws.String("i-123"),
						PingStatus: ssmtypes.PingStatusOnline,
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	diags := waitForSSMInstanceReadiness(ctx, client, []string{"i-123"})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics errors, got: %v", diags)
	}
	if client.describeCalls != 1 {
		t.Fatalf("expected one DescribeInstanceInformation call, got: %d", client.describeCalls)
	}
}

func TestWaitForSSMInstanceReadinessRetriesUntilOnline(t *testing.T) {
	setRetryBackoffForTest(t)

	client := &fakeSSMClient{
		describeOutputs: []*ssm.DescribeInstanceInformationOutput{
			{
				InstanceInformationList: []ssmtypes.InstanceInformation{
					{
						InstanceId: aws.String("i-123"),
						PingStatus: ssmtypes.PingStatusConnectionLost,
					},
				},
			},
			{
				InstanceInformationList: []ssmtypes.InstanceInformation{
					{
						InstanceId: aws.String("i-123"),
						PingStatus: ssmtypes.PingStatusOnline,
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	diags := waitForSSMInstanceReadiness(ctx, client, []string{"i-123"})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics errors, got: %v", diags)
	}
	if client.describeCalls < 2 {
		t.Fatalf("expected at least two DescribeInstanceInformation calls, got: %d", client.describeCalls)
	}
}

func TestWaitForSSMInstanceReadinessTimeout(t *testing.T) {
	setRetryBackoffForTest(t)

	client := &fakeSSMClient{
		describeOutputs: []*ssm.DescribeInstanceInformationOutput{
			{
				InstanceInformationList: []ssmtypes.InstanceInformation{},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	diags := waitForSSMInstanceReadiness(ctx, client, []string{"i-123"})
	if !diags.HasError() {
		t.Fatalf("expected readiness timeout to return diagnostics error")
	}
}

func TestSendCommandWithRetryTransientThenSuccess(t *testing.T) {
	setRetryBackoffForTest(t)

	client := &fakeSSMClient{
		sendErrs: []error{
			mockAPIError{
				code: "InvalidInstanceId",
				msg:  "Instance is not in valid state for account",
			},
		},
		sendOutputs: []*ssm.SendCommandOutput{
			{
				Command: &ssmtypes.Command{
					CommandId: aws.String("cmd-123"),
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	command, diags := sendCommandWithRetry(ctx, client, &ssm.SendCommandInput{}, "doc", []string{"i-123"})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics errors, got: %v", diags)
	}
	if command == nil || command.Command == nil || command.Command.CommandId == nil {
		t.Fatalf("expected command response with id, got: %#v", command)
	}
	if client.sendCalls != 2 {
		t.Fatalf("expected two SendCommand calls, got: %d", client.sendCalls)
	}
}

func TestSendCommandWithRetryNonRetriableError(t *testing.T) {
	setRetryBackoffForTest(t)

	client := &fakeSSMClient{
		sendErrs: []error{
			errors.New("validation failed"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, diags := sendCommandWithRetry(ctx, client, &ssm.SendCommandInput{}, "doc", []string{"i-123"})
	if !diags.HasError() {
		t.Fatalf("expected diagnostics error for non-retriable send failure")
	}
	if client.sendCalls != 1 {
		t.Fatalf("expected one SendCommand call, got: %d", client.sendCalls)
	}
}

func TestIsRetriableSendCommandReadinessError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name: "api error invalid instance id readiness",
			err: mockAPIError{
				code: "InvalidInstanceId",
				msg:  "instance i-123 is not in valid state for account 123",
			},
			expected: true,
		},
		{
			name: "api error invalid instance id non readiness",
			err: mockAPIError{
				code: "InvalidInstanceId",
				msg:  "you specified an instance id that does not exist",
			},
			expected: false,
		},
		{
			name:     "string error readiness marker",
			err:      errors.New("No instances in target list are managed instances"),
			expected: true,
		},
		{
			name:     "random error",
			err:      errors.New("random failure"),
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := isRetriableSendCommandReadinessError(testCase.err)
			if got != testCase.expected {
				t.Fatalf("expected %t, got %t", testCase.expected, got)
			}
		})
	}
}

func setRetryBackoffForTest(t *testing.T) {
	t.Helper()

	backoffSettingsMu.Lock()
	originalInitial := initialRetryBackoff
	originalMax := maxRetryBackoff
	initialRetryBackoff = 1 * time.Millisecond
	maxRetryBackoff = 2 * time.Millisecond
	backoffSettingsMu.Unlock()

	t.Cleanup(func() {
		backoffSettingsMu.Lock()
		initialRetryBackoff = originalInitial
		maxRetryBackoff = originalMax
		backoffSettingsMu.Unlock()
	})
}

type fakeSSMClient struct {
	sendCalls   int
	sendErrs    []error
	sendOutputs []*ssm.SendCommandOutput

	describeCalls   int
	describeErrs    []error
	describeOutputs []*ssm.DescribeInstanceInformationOutput
}

func (f *fakeSSMClient) SendCommand(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	callIndex := f.sendCalls
	f.sendCalls++

	if callIndex < len(f.sendErrs) && f.sendErrs[callIndex] != nil {
		return nil, f.sendErrs[callIndex]
	}

	outputIndex := callIndex - len(f.sendErrs)
	if outputIndex >= 0 && outputIndex < len(f.sendOutputs) {
		return f.sendOutputs[outputIndex], nil
	}

	if len(f.sendOutputs) > 0 {
		return f.sendOutputs[len(f.sendOutputs)-1], nil
	}

	return &ssm.SendCommandOutput{}, nil
}

func (f *fakeSSMClient) ListCommandInvocations(ctx context.Context, params *ssm.ListCommandInvocationsInput, optFns ...func(*ssm.Options)) (*ssm.ListCommandInvocationsOutput, error) {
	return &ssm.ListCommandInvocationsOutput{}, nil
}

func (f *fakeSSMClient) DescribeInstanceInformation(ctx context.Context, params *ssm.DescribeInstanceInformationInput, optFns ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error) {
	callIndex := f.describeCalls
	f.describeCalls++

	if callIndex < len(f.describeErrs) && f.describeErrs[callIndex] != nil {
		return nil, f.describeErrs[callIndex]
	}

	if callIndex < len(f.describeOutputs) {
		return f.describeOutputs[callIndex], nil
	}

	if len(f.describeOutputs) > 0 {
		return f.describeOutputs[len(f.describeOutputs)-1], nil
	}

	return &ssm.DescribeInstanceInformationOutput{}, nil
}

type mockAPIError struct {
	code string
	msg  string
}

func (e mockAPIError) Error() string {
	return e.code + ": " + e.msg
}

func (e mockAPIError) ErrorCode() string {
	return e.code
}

func (e mockAPIError) ErrorMessage() string {
	return e.msg
}

func (e mockAPIError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultClient
}
