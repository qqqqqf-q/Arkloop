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

var ContextAgentSpec = tools.AgentToolSpec{
	Name:        "conversation_context",
	Version:     "1",
	Description: "inspect and expand compacted context for the current thread",
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

var ThreadListAgentSpec = tools.AgentToolSpec{
	Name:        "thread_list",
	Version:     "1",
	Description: "list the current user's conversation threads",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var ThreadListLlmSpec = llm.ToolSpec{
	Name:        "thread_list",
	Description: stringPtr(sharedtoolmeta.Must("thread_list").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 30},
			"offset": map[string]any{"type": "integer", "minimum": 0},
			"mode":   map[string]any{"type": "string", "enum": []string{"chat", "work"}},
		},
		"additionalProperties": false,
	},
}

var ThreadMessagesAgentSpec = tools.AgentToolSpec{
	Name:        "thread_messages",
	Version:     "1",
	Description: "read messages from a specific conversation thread",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var ThreadMessagesLlmSpec = llm.ToolSpec{
	Name:        "thread_messages",
	Description: stringPtr(sharedtoolmeta.Must("thread_messages").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"thread_id": map[string]any{"type": "string"},
			"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			"offset":    map[string]any{"type": "integer", "minimum": 0},
			"role":      map[string]any{"type": "string", "enum": []string{"user", "assistant"}},
			"order":     map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
		},
		"required":             []string{"thread_id"},
		"additionalProperties": false,
	},
}

var ContextLlmSpec = llm.ToolSpec{
	Name:        "conversation_context",
	Description: stringPtr(sharedtoolmeta.Must("conversation_context").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"status", "search", "describe", "expand"},
			},
			"query": map[string]any{"type": "string"},
			"scope": map[string]any{
				"type": "string",
				"enum": []string{"summaries", "chunks", "both"},
			},
			"replacement_id": map[string]any{"type": "string"},
			"limit":          map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			"token_cap":      map[string]any{"type": "integer", "minimum": 256, "maximum": 12000},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{SearchAgentSpec, ContextAgentSpec, ThreadListAgentSpec, ThreadMessagesAgentSpec}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{SearchLlmSpec, ContextLlmSpec, ThreadListLlmSpec, ThreadMessagesLlmSpec}
}
