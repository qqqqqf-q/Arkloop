package resourcecopy

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "resource_copy"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "copy an artifact or message attachment into the agent filesystem",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr(sharedtoolmeta.Must(ToolName).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_uri": map[string]any{
				"type":        "string",
				"description": "resource URI to copy, e.g. artifact:<key> or attachment:<key>",
			},
			"target_path": map[string]any{
				"type":        "string",
				"description": "absolute file path inside the active work directory, e.g. /workspace/input.png",
			},
		},
		"required":             []string{"source_uri", "target_path"},
		"additionalProperties": false,
	},
}

func strPtr(value string) *string { return &value }
