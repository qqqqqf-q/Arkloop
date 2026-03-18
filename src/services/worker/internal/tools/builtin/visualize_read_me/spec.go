package visualizereadme

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	generativeuisource "arkloop/services/worker/internal/tools/builtin/generative_ui_source"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "visualize_read_me",
	Version:     "1",
	Description: "load the canonical generative UI design guidelines",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "visualize_read_me",
	Description: strPtr(sharedtoolmeta.Must("visualize_read_me").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"modules": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
					"enum": generativeuisource.AvailableModules(),
				},
				"description": "which guideline module(s) to load",
			},
		},
		"required":             []string{"modules"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
