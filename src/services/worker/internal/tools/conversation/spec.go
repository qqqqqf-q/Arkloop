package conversation

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

func stringPtr(s string) *string { return &s }

var SearchAgentSpec = tools.AgentToolSpec{
	Name:        "conversation_search",
	Version:     "1",
	Description: "search visible conversation history for the current user",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var SearchLlmSpec = llm.ToolSpec{
	Name:        "conversation_search",
	Description: stringPtr(sharedtoolmeta.Must("conversation_search").LLMDescription),
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

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{SearchAgentSpec}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{SearchLlmSpec}
}
