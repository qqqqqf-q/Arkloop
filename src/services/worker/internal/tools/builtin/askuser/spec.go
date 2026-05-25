package askuser

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "ask_user"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "3",
	Description: "ask the user questions via structured form",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

// fieldDef: 单个表单字段的 schema。
// 使用扁平数组而非 additionalProperties 对象，确保 OpenAI function calling 兼容。
var fieldDef = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"key": map[string]any{
			"type":        "string",
			"description": "Unique identifier for this field. Used as the key in the user's response object.",
		},
		"type": map[string]any{
			"type":        "string",
			"enum":        []string{"string", "boolean", "number", "integer", "array"},
			"description": "Field type. string=text/select, boolean=toggle, number/integer=numeric, array=multiselect.",
		},
		"title":       map[string]any{"type": "string", "description": "Label shown to the user."},
		"description": map[string]any{"type": "string", "description": "Additional help text."},
		"enum": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Options for select (type=string) or multiselect (type=array).",
		},
		"enumNames": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Display labels for enum values, same order as enum.",
		},
		"oneOf": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"const": map[string]any{"type": "string"},
					"title": map[string]any{"type": "string"},
				},
				"required": []string{"const", "title"},
			},
			"description": "Labeled options with distinct value/label. Use instead of enum when display text differs from value.",
		},
		"required": map[string]any{
			"type":        "boolean",
			"description": "Whether the user must fill this field. Default false.",
		},
		"default":   map[string]any{"description": "Default value for the field."},
		"minimum":   map[string]any{"type": "number"},
		"maximum":   map[string]any{"type": "number"},
		"minLength": map[string]any{"type": "number"},
		"maxLength": map[string]any{"type": "number"},
		"minItems":  map[string]any{"type": "number"},
		"maxItems":  map[string]any{"type": "number"},
	},
	"required": []string{"key", "type"},
}

var LlmSpec = llm.ToolSpec{
	Name: ToolName,
	Description: strPtr(
		"Ask the user questions and wait for their response. " +
			"Use this when you need user decisions, confirmations, or input. " +
			"Define multiple fields to ask multiple questions in a single form. " +
			"Field types: string+enum=select, string=text, boolean=toggle, number=numeric, array+enum=multiselect.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "A clear message describing what you need from the user.",
			},
			"display_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"inline", "form"},
				"description": "How to present the question. inline for quick confirmations/approvals that don't need a record; form for collecting detailed input that should persist in chat history for later review.",
			},
			"fields": map[string]any{
				"type":        "array",
				"description": "Form field definitions. Each item is a field rendered in the form.",
				"items":       fieldDef,
			},
		},
		"required": []string{"message", "fields"},
	},
}

func strPtr(s string) *string { return &s }
