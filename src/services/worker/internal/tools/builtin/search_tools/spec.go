package searchtools

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "search_tools",
	Version:     "1",
	Description: "look up tools in this runtime catalog by tool name or catalog keyword (not web search)",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "search_tools",
	Description: llmStringPtr(sharedtoolmeta.Must("search_tools").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type": "array",
				"description": "each string must be a tool name or a word from this platform's tool catalog metadata — not a research question or web query. " +
					"Multiple entries are resolved in one call. Use [\"*\"] to load all searchable tools at once.",
				"minItems":    1,
				"maxItems":    10,
				"items":       map[string]any{"type": "string"},
			},
		},
		"required":             []string{"queries"},
		"additionalProperties": false,
	},
}

func llmStringPtr(s string) *string { return &s }
