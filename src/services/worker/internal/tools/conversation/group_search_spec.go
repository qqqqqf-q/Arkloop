package conversation

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var GroupSearchAgentSpec = tools.AgentToolSpec{
	Name:        "group_history_search",
	Version:     "1",
	Description: "keyword-search group chat history within the current thread",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var GroupSearchLlmSpec = llm.ToolSpec{
	Name:        "group_history_search",
	Description: stringPtr(sharedtoolmeta.Must("group_history_search").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	},
}
