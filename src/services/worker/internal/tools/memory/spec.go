package memory

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

func stringPtr(s string) *string { return &s }

// --- memory_search ---

var SearchAgentSpec = tools.AgentToolSpec{
	Name:        "memory_search",
	Version:     "1",
	Description: "search long-term memory for relevant information",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var SearchLlmSpec = llm.ToolSpec{
	Name:        "memory_search",
	Description: stringPtr("search your long-term memory about the user or past interactions"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "minLength": 1},
			"scope": map[string]any{"type": "string", "enum": []string{"user", "agent"}},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	},
}

// --- memory_read ---

var ReadAgentSpec = tools.AgentToolSpec{
	Name:        "memory_read",
	Version:     "1",
	Description: "read detailed content of a memory entry by URI",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var ReadLlmSpec = llm.ToolSpec{
	Name:        "memory_read",
	Description: stringPtr("read detailed content of a memory entry by URI"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri":   map[string]any{"type": "string", "minLength": 1},
			"depth": map[string]any{"type": "string", "enum": []string{"overview", "full"}},
		},
		"required":             []string{"uri"},
		"additionalProperties": false,
	},
}

// --- memory_write ---

var WriteAgentSpec = tools.AgentToolSpec{
	Name:        "memory_write",
	Version:     "1",
	Description: "store a piece of knowledge in long-term memory for future reference",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var WriteLlmSpec = llm.ToolSpec{
	Name:        "memory_write",
	Description: stringPtr("store a piece of knowledge in long-term memory for future reference"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"category": map[string]any{
				"type": "string",
				"enum": []string{"profile", "preferences", "entities", "events", "cases", "patterns"},
			},
			"key":     map[string]any{"type": "string", "minLength": 1, "pattern": `^[a-zA-Z0-9_\-\.]+$`},
			"content": map[string]any{"type": "string", "minLength": 1},
			"scope":   map[string]any{"type": "string", "enum": []string{"user", "agent"}},
		},
		"required":             []string{"category", "key", "content"},
		"additionalProperties": false,
	},
}

// --- memory_forget ---

var ForgetAgentSpec = tools.AgentToolSpec{
	Name:        "memory_forget",
	Version:     "1",
	Description: "remove a specific memory entry",
	// Medium：删除操作不可逆，比只读的 search/read 高一级
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var ForgetLlmSpec = llm.ToolSpec{
	Name:        "memory_forget",
	Description: stringPtr("remove a specific memory entry"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri": map[string]any{"type": "string", "minLength": 1},
		},
		"required":             []string{"uri"},
		"additionalProperties": false,
	},
}

// AgentSpecs 返回全部 memory tool 的 AgentToolSpec。
func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		SearchAgentSpec,
		ReadAgentSpec,
		WriteAgentSpec,
		ForgetAgentSpec,
	}
}

// LlmSpecs 返回全部 memory tool 的 LlmToolSpec。
func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		SearchLlmSpec,
		ReadLlmSpec,
		WriteLlmSpec,
		ForgetLlmSpec,
	}
}
