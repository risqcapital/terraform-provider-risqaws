package provider

import (
	"context"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func TestJSONEncodeFunctionRunCompact(t *testing.T) {
	t.Parallel()

	resp := runJSONEncodeFunction(t, testJSONValue())

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.Value().(basetypes.StringValue)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result.Value())
	}

	const expected = "{\"enabled\":true,\"name\":\"example\",\"nested\":{\"items\":[\"a\",\"b\"],\"value\":2.5}}"

	if result.ValueString() != expected {
		t.Fatalf("unexpected compact JSON\nexpected: %s\nactual:   %s", expected, result.ValueString())
	}
}

func TestJSONEncodeFunctionRunIndented(t *testing.T) {
	t.Parallel()

	resp := runJSONEncodeFunction(t, testJSONValue(), types.StringValue("  "))

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.Value().(basetypes.StringValue)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result.Value())
	}

	const expected = "{\n  \"enabled\": true,\n  \"name\": \"example\",\n  \"nested\": {\n    \"items\": [\n      \"a\",\n      \"b\"\n    ],\n    \"value\": 2.5\n  }\n}"

	if result.ValueString() != expected {
		t.Fatalf("unexpected indented JSON\nexpected: %s\nactual:   %s", expected, result.ValueString())
	}
}

func TestJSONEncodeFunctionRunUnknownValue(t *testing.T) {
	t.Parallel()

	resp := runJSONEncodeFunction(t, types.DynamicValue(types.ObjectValueMust(
		map[string]attr.Type{"name": types.StringType},
		map[string]attr.Value{"name": types.StringUnknown()},
	)))

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.Value().(basetypes.StringValue)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result.Value())
	}

	if !result.Equal(types.StringUnknown()) {
		t.Fatalf("expected unknown string result, got %s", result.String())
	}
}

func TestJSONEncodeFunctionRunTooManyIndentArguments(t *testing.T) {
	t.Parallel()

	resp := runJSONEncodeFunction(t, testJSONValue(), types.StringValue("  "), types.StringValue("\t"))

	if resp.Error == nil {
		t.Fatal("expected error but got none")
	}

	const expected = "The optional indent argument may be specified at most once."

	if resp.Error.Error() != expected {
		t.Fatalf("unexpected error\nexpected: %s\nactual:   %s", expected, resp.Error.Error())
	}
	if resp.Error.FunctionArgument == nil || *resp.Error.FunctionArgument != 1 {
		t.Fatalf("expected error to reference argument 1, got %#v", resp.Error.FunctionArgument)
	}
}

func runJSONEncodeFunction(t *testing.T, args ...attr.Value) function.RunResponse {
	t.Helper()

	ctx := context.Background()
	if len(args) == 0 {
		t.Fatal("runJSONEncodeFunction requires at least one argument")
	}

	variadicElementTypes := make([]attr.Type, 0, max(len(args)-1, 0))
	variadicValues := make([]attr.Value, 0, max(len(args)-1, 0))
	for _, arg := range args[1:] {
		variadicElementTypes = append(variadicElementTypes, arg.Type(ctx))
		variadicValues = append(variadicValues, arg)
	}

	resp := function.RunResponse{
		Result: function.NewResultData(types.StringUnknown()),
	}

	NewJSONEncodeFunction().Run(ctx, function.RunRequest{
		Arguments: function.NewArgumentsData([]attr.Value{
			args[0],
			types.TupleValueMust(variadicElementTypes, variadicValues),
		}),
	}, &resp)

	return resp
}

func testJSONValue() attr.Value {
	return types.DynamicValue(types.ObjectValueMust(
		map[string]attr.Type{
			"enabled": types.BoolType,
			"name":    types.StringType,
			"nested": types.ObjectType{
				AttrTypes: map[string]attr.Type{
					"items": types.ListType{ElemType: types.StringType},
					"value": types.NumberType,
				},
			},
		},
		map[string]attr.Value{
			"enabled": types.BoolValue(true),
			"name":    types.StringValue("example"),
			"nested": types.ObjectValueMust(
				map[string]attr.Type{
					"items": types.ListType{ElemType: types.StringType},
					"value": types.NumberType,
				},
				map[string]attr.Value{
					"items": types.ListValueMust(types.StringType, []attr.Value{
						types.StringValue("a"),
						types.StringValue("b"),
					}),
					"value": types.NumberValue(big.NewFloat(2.5)),
				},
			),
		},
	))
}
