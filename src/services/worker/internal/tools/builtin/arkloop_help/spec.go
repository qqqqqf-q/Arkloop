package arkloophelp

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "arkloop_help",
	Version:     "1",
	Description: "search embedded Arkloop product and desktop help documentation",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "arkloop_help",
	Description: strPtr(sharedtoolmeta.Must("arkloop_help").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "natural language question or keywords about Arkloop product, architecture, Desktop, or Telegram channels",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "max chunks to return (default 4, max 12)",
				"minimum":     1,
				"maximum":     12,
			},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
