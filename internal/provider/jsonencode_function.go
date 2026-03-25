// Copyright (c) Risq Research Ltd.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

var _ function.Function = &JSONEncodeFunction{}

// JSONEncodeFunction returns compact JSON by default and accepts an optional
// indentation string as a second argument for formatted output.
type JSONEncodeFunction struct{}

func NewJSONEncodeFunction() function.Function {
	return &JSONEncodeFunction{}
}

func (f *JSONEncodeFunction) Metadata(ctx context.Context, req function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "jsonencode"
}

func (f *JSONEncodeFunction) Definition(ctx context.Context, req function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary:             "Encode a value as JSON, optionally with indentation.",
		Description:         "Returns a JSON string for the given value. Omit the second argument to match Terraform's compact jsonencode output, or pass an indentation string such as two spaces or a tab to produce formatted JSON.",
		MarkdownDescription: "Returns a JSON string for the given value. Omit the second argument to match Terraform's compact `jsonencode` output, or pass an indentation string such as two spaces or a tab to produce formatted JSON.\n\n## Example Usage\n\n```hcl\nlocals {\n  data = {\n    foo = \"bar\"\n    fiz = {\n      fizzy = \"buzzy\"\n    }\n  }\n\n  indent        = \"  \"\n  indented_json = provider::risqaws::jsonencode(local.data, local.indent)\n}\n```\n\nThis produces:\n\n```json\n{\n  \"foo\": \"bar\",\n  \"fiz\": {\n    \"fizzy\": \"buzzy\"\n  }\n}\n```",
		Parameters: []function.Parameter{
			function.DynamicParameter{
				Name:               "value",
				AllowNullValue:     true,
				AllowUnknownValues: true,
			},
		},
		VariadicParameter: function.StringParameter{
			Name:               "indent",
			AllowUnknownValues: true,
		},
		Return: function.StringReturn{},
	}
}

func (f *JSONEncodeFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var value types.Dynamic
	var indents []types.String

	resp.Error = req.Arguments.Get(ctx, &value, &indents)
	if resp.Error != nil {
		return
	}

	if len(indents) > 1 {
		resp.Error = function.NewArgumentFuncError(1, "The optional indent argument may be specified at most once.")
		return
	}

	if len(indents) == 1 && indents[0].IsUnknown() {
		resp.Error = resp.Result.Set(ctx, types.StringUnknown())
		return
	}

	tfValue, err := value.ToTerraformValue(ctx)
	if err != nil {
		resp.Error = function.NewArgumentFuncError(0, fmt.Sprintf("Unable to read the input value: %v", err))
		return
	}

	if !tfValue.IsFullyKnown() {
		resp.Error = resp.Result.Set(ctx, types.StringUnknown())
		return
	}

	indent := ""
	if len(indents) == 1 {
		indent = indents[0].ValueString()
	}

	jsonValue, err := encodeJSONValue(ctx, value, indent)
	if err != nil {
		resp.Error = function.NewArgumentFuncError(0, fmt.Sprintf("Unable to encode the input value as JSON: %v", err))
		return
	}

	resp.Error = resp.Result.Set(ctx, string(jsonValue))
}

func encodeJSONValue(ctx context.Context, value attr.Value, indent string) ([]byte, error) {
	ctyValue, err := attrValueToCTYValue(ctx, value)
	if err != nil {
		return nil, err
	}

	jsonValue, err := ctyjson.Marshal(ctyValue, ctyValue.Type())
	if err != nil {
		return nil, err
	}

	jsonValue = bytes.TrimSpace(jsonValue)

	if indent == "" {
		return jsonValue, nil
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, jsonValue, "", indent); err != nil {
		return nil, err
	}

	return formatted.Bytes(), nil
}

func attrTypeToCTYType(valueType attr.Type) (cty.Type, error) {
	switch valueType := valueType.(type) {
	case basetypes.StringType:
		return cty.String, nil
	case basetypes.BoolType:
		return cty.Bool, nil
	case basetypes.NumberType:
		return cty.Number, nil
	case basetypes.DynamicType:
		return cty.DynamicPseudoType, nil
	case basetypes.ListType:
		elementType, err := attrTypeToCTYType(valueType.ElementType())
		if err != nil {
			return cty.NilType, err
		}

		return cty.List(elementType), nil
	case basetypes.SetType:
		elementType, err := attrTypeToCTYType(valueType.ElementType())
		if err != nil {
			return cty.NilType, err
		}

		return cty.Set(elementType), nil
	case basetypes.MapType:
		elementType, err := attrTypeToCTYType(valueType.ElementType())
		if err != nil {
			return cty.NilType, err
		}

		return cty.Map(elementType), nil
	case basetypes.ObjectType:
		attributeTypes := make(map[string]cty.Type, len(valueType.AttributeTypes()))

		for name, attributeType := range valueType.AttributeTypes() {
			ctyType, err := attrTypeToCTYType(attributeType)
			if err != nil {
				return cty.NilType, err
			}

			attributeTypes[name] = ctyType
		}

		return cty.Object(attributeTypes), nil
	case basetypes.TupleType:
		elementTypes := make([]cty.Type, 0, len(valueType.ElementTypes()))

		for _, elementType := range valueType.ElementTypes() {
			ctyType, err := attrTypeToCTYType(elementType)
			if err != nil {
				return cty.NilType, err
			}

			elementTypes = append(elementTypes, ctyType)
		}

		return cty.Tuple(elementTypes), nil
	default:
		return cty.NilType, fmt.Errorf("unsupported Terraform type %T", valueType)
	}
}

func attrValueToCTYValue(ctx context.Context, value attr.Value) (cty.Value, error) {
	switch value := value.(type) {
	case basetypes.DynamicValue:
		if value.IsUnknown() || value.IsUnderlyingValueUnknown() {
			return cty.UnknownVal(cty.DynamicPseudoType), nil
		}

		if value.IsNull() {
			return cty.NullVal(cty.DynamicPseudoType), nil
		}

		underlyingValue := value.UnderlyingValue()
		if underlyingValue == nil {
			return cty.NullVal(cty.DynamicPseudoType), nil
		}

		return attrValueToCTYValue(ctx, underlyingValue)
	case basetypes.StringValue:
		if value.IsUnknown() {
			return cty.UnknownVal(cty.String), nil
		}

		if value.IsNull() {
			return cty.NullVal(cty.String), nil
		}

		return cty.StringVal(value.ValueString()), nil
	case basetypes.BoolValue:
		if value.IsUnknown() {
			return cty.UnknownVal(cty.Bool), nil
		}

		if value.IsNull() {
			return cty.NullVal(cty.Bool), nil
		}

		return cty.BoolVal(value.ValueBool()), nil
	case basetypes.NumberValue:
		if value.IsUnknown() {
			return cty.UnknownVal(cty.Number), nil
		}

		if value.IsNull() {
			return cty.NullVal(cty.Number), nil
		}

		return cty.NumberVal(value.ValueBigFloat()), nil
	case basetypes.ListValue:
		listType, err := attrTypeToCTYType(value.Type(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if value.IsUnknown() {
			return cty.UnknownVal(listType), nil
		}

		if value.IsNull() {
			return cty.NullVal(listType), nil
		}

		elements := make([]cty.Value, 0, len(value.Elements()))
		for _, element := range value.Elements() {
			ctyElement, err := attrValueToCTYValue(ctx, element)
			if err != nil {
				return cty.NilVal, err
			}

			elements = append(elements, ctyElement)
		}

		elementType, err := attrTypeToCTYType(value.ElementType(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if len(elements) == 0 {
			return cty.ListValEmpty(elementType), nil
		}

		return cty.ListVal(elements), nil
	case basetypes.SetValue:
		setType, err := attrTypeToCTYType(value.Type(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if value.IsUnknown() {
			return cty.UnknownVal(setType), nil
		}

		if value.IsNull() {
			return cty.NullVal(setType), nil
		}

		elements := make([]cty.Value, 0, len(value.Elements()))
		for _, element := range value.Elements() {
			ctyElement, err := attrValueToCTYValue(ctx, element)
			if err != nil {
				return cty.NilVal, err
			}

			elements = append(elements, ctyElement)
		}

		elementType, err := attrTypeToCTYType(value.ElementType(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if len(elements) == 0 {
			return cty.SetValEmpty(elementType), nil
		}

		return cty.SetVal(elements), nil
	case basetypes.MapValue:
		mapType, err := attrTypeToCTYType(value.Type(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if value.IsUnknown() {
			return cty.UnknownVal(mapType), nil
		}

		if value.IsNull() {
			return cty.NullVal(mapType), nil
		}

		elements := make(map[string]cty.Value, len(value.Elements()))
		for name, element := range value.Elements() {
			ctyElement, err := attrValueToCTYValue(ctx, element)
			if err != nil {
				return cty.NilVal, err
			}

			elements[name] = ctyElement
		}

		elementType, err := attrTypeToCTYType(value.ElementType(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if len(elements) == 0 {
			return cty.MapValEmpty(elementType), nil
		}

		return cty.MapVal(elements), nil
	case basetypes.ObjectValue:
		objectType, err := attrTypeToCTYType(value.Type(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if value.IsUnknown() {
			return cty.UnknownVal(objectType), nil
		}

		if value.IsNull() {
			return cty.NullVal(objectType), nil
		}

		attributes := make(map[string]cty.Value, len(value.Attributes()))
		for name, attribute := range value.Attributes() {
			ctyAttribute, err := attrValueToCTYValue(ctx, attribute)
			if err != nil {
				return cty.NilVal, err
			}

			attributes[name] = ctyAttribute
		}

		if len(attributes) == 0 {
			return cty.EmptyObjectVal, nil
		}

		return cty.ObjectVal(attributes), nil
	case basetypes.TupleValue:
		tupleType, err := attrTypeToCTYType(value.Type(ctx))
		if err != nil {
			return cty.NilVal, err
		}

		if value.IsUnknown() {
			return cty.UnknownVal(tupleType), nil
		}

		if value.IsNull() {
			return cty.NullVal(tupleType), nil
		}

		elements := make([]cty.Value, 0, len(value.Elements()))
		for _, element := range value.Elements() {
			ctyElement, err := attrValueToCTYValue(ctx, element)
			if err != nil {
				return cty.NilVal, err
			}

			elements = append(elements, ctyElement)
		}

		if len(elements) == 0 {
			return cty.EmptyTupleVal, nil
		}

		return cty.TupleVal(elements), nil
	default:
		return cty.NilVal, fmt.Errorf("unsupported Terraform value type %T", value)
	}
}
