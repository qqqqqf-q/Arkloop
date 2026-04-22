package edit

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "edit",
	Version:     "1",
	Description: "replace a unique string in a file (str_replace semantics)",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "edit",
	Description: strPtr(sharedtoolmeta.Must("edit").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "path to the file to edit",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "exact text to find and replace (must match exactly once in the file)",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "replacement text",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "if true, replace all occurrences of old_string (default false)",
			},
		},
		"required":             []string{"file_path", "old_string", "new_string"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
