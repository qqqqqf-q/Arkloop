package askuser

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid = "tool.args_invalid"
	maxFields        = 10
)

type ToolExecutor struct{}

func (ToolExecutor) Execute(
	_ context.Context,
	_ string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	message, schema, err := ValidateAndNormalize(args)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    err.Error(),
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"status":          "pending_user_input",
			"message":         message,
			"requestedSchema": schema,
		},
		DurationMs: durationMs(started),
	}
}

// ValidateAndNormalize 验证 LLM 传入的 {message, fields} 格式，
// 并转换为前端期望的 {properties: {...}, required: [...]} 格式。
func ValidateAndNormalize(args map[string]any) (string, map[string]any, error) {
	message, _ := args["message"].(string)
	if message == "" {
		return "", nil, fmt.Errorf("missing required field: message")
	}

	displayMode, _ := args["display_mode"].(string)
	displayMode = strings.TrimSpace(displayMode)
	if displayMode == "" {
		displayMode = "inline"
	}
	if displayMode != "inline" && displayMode != "form" {
		return "", nil, fmt.Errorf("display_mode must be one of inline, form")
	}

	fieldsRaw, ok := args["fields"]
	if !ok {
		return "", nil, fmt.Errorf("missing required field: fields")
	}
	fields, ok := fieldsRaw.([]any)
	if !ok || len(fields) == 0 {
		return "", nil, fmt.Errorf("fields must be a non-empty array")
	}
	if len(fields) > maxFields {
		return "", nil, fmt.Errorf("fields must have at most %d entries", maxFields)
	}

	properties := make(map[string]any)
	var requiredKeys []any
	var orderedKeys []any

	for i, fieldRaw := range fields {
		field, ok := fieldRaw.(map[string]any)
		if !ok {
			return "", nil, fmt.Errorf("fields[%d] must be an object", i)
		}

		key, _ := field["key"].(string)
		if key == "" {
			return "", nil, fmt.Errorf("fields[%d] must have a non-empty key", i)
		}
		if _, dup := properties[key]; dup {
			return "", nil, fmt.Errorf("duplicate field key: %q", key)
		}

		prop, err := buildProperty(key, field)
		if err != nil {
			return "", nil, err
		}
		properties[key] = prop
		orderedKeys = append(orderedKeys, key)

		if req, _ := field["required"].(bool); req {
			requiredKeys = append(requiredKeys, key)
		}
	}

	schema := map[string]any{
		"properties":   properties,
		"display_mode": displayMode,
	}
	if len(orderedKeys) > 0 {
		schema["_fieldOrder"] = orderedKeys
	}
	if len(requiredKeys) > 0 {
		schema["required"] = requiredKeys
	}
	return message, schema, nil
}

// buildProperty 从 LLM 字段定义构建前端属性 schema
func buildProperty(key string, field map[string]any) (map[string]any, error) {
	typ, _ := field["type"].(string)
	if typ == "" {
		return nil, fmt.Errorf("field %q must have a type", key)
	}

	prop := map[string]any{"type": typ}

	// 复制可选元数据
	for _, k := range []string{"title", "description", "default", "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"} {
		if v, ok := field[k]; ok {
			prop[k] = v
		}
	}

	switch typ {
	case "string":
		return normalizeStringProp(key, field, prop)
	case "array":
		return normalizeArrayProp(key, field, prop)
	case "boolean", "number", "integer":
		return prop, nil
	default:
		return nil, fmt.Errorf("field %q has unsupported type %q", key, typ)
	}
}

func normalizeStringProp(key string, field, prop map[string]any) (map[string]any, error) {
	if enumVal, ok := field["enum"]; ok {
		if err := validateEnumArray(key, enumVal); err != nil {
			return nil, err
		}
		prop["enum"] = enumVal
		if names, ok := field["enumNames"]; ok {
			prop["enumNames"] = names
		}
	}
	if oneOfVal, ok := field["oneOf"]; ok {
		if err := validateOneOfItems(key, oneOfVal); err != nil {
			return nil, err
		}
		prop["oneOf"] = oneOfVal
	}
	return prop, nil
}

func normalizeArrayProp(key string, field, prop map[string]any) (map[string]any, error) {
	enumVal, hasEnum := field["enum"]
	oneOfVal, hasOneOf := field["oneOf"]

	if hasEnum {
		if err := validateEnumArray(key, enumVal); err != nil {
			return nil, err
		}
		prop["items"] = map[string]any{"type": "string", "enum": enumVal}
		return prop, nil
	}
	if hasOneOf {
		if err := validateOneOfItems(key, oneOfVal); err != nil {
			return nil, err
		}
		prop["items"] = map[string]any{"anyOf": oneOfVal}
		return prop, nil
	}
	return nil, fmt.Errorf("field %q: array type must have enum or oneOf", key)
}

func validateEnumArray(key string, raw any) error {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return fmt.Errorf("field %q: enum must be a non-empty array", key)
	}
	for i, v := range arr {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("field %q: enum[%d] must be a string", key, i)
		}
	}
	return nil
}

func validateOneOfItems(key string, raw any) error {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return fmt.Errorf("field %q: oneOf must be a non-empty array", key)
	}
	for i, v := range arr {
		item, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("field %q: oneOf[%d] must be an object", key, i)
		}
		c, _ := item["const"].(string)
		t, _ := item["title"].(string)
		if c == "" || t == "" {
			return fmt.Errorf("field %q: oneOf[%d] must have non-empty const and title", key, i)
		}
	}
	return nil
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}
