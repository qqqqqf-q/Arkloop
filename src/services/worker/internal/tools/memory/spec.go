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
	Description: stringPtr("search long-term memory for information about the user (preferences, past experiences, constraints, priorities) or past interactions. Use this tool when handling recommendations, comparisons, preference-driven questions, opinions, 'best' options, 'how to' questions, or open-ended problems with multiple valid approaches — user context significantly improves answer quality in areas like shopping, travel planning, and project planning. Call at most once per user query; do not issue multiple memory searches for the same request. Use the results to guide subsequent tool selection — memory provides context, but a complete answer may still require other tools. IMPORTANT: results contain internal fields (such as uri, _ref) that are system identifiers and must never be shown to the user; only present the natural-language content (abstract) to the user, never expose storage paths, URIs, or any internal metadata."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
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
	Description: stringPtr("read the full content of a memory entry by its URI. IMPORTANT: the URI and other internal fields (_ref, storage paths) are system identifiers and must never be exposed to the user; only present the natural-language content to the user."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri":   map[string]any{"type": "string"},
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
			"key":     map[string]any{"type": "string", "pattern": `^[a-zA-Z0-9_\-\.]+$`},
			"content": map[string]any{"type": "string"},
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
			"uri": map[string]any{"type": "string"},
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
