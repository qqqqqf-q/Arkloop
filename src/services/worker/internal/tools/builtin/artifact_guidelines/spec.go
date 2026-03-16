package artifactguidelines

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "artifact_guidelines",
	Version:     "1",
	Description: "load design guidelines for artifact creation",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "artifact_guidelines",
	Description: strPtr(sharedtoolmeta.Must("artifact_guidelines").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"modules": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
					"enum": []string{"interactive", "chart", "diagram", "art"},
				},
				"description": "which design guideline modules to load",
			},
		},
		"required":             []string{"modules"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
