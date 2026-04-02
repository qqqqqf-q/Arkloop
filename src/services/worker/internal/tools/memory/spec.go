package memory

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
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
	Description: stringPtr(sharedtoolmeta.Must("memory_search").LLMDescription),
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
	Description: stringPtr(sharedtoolmeta.Must("memory_read").LLMDescription),
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
	Description: stringPtr(sharedtoolmeta.Must("memory_write").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"category": map[string]any{
				"type": "string",
				"enum": []string{"profile", "preferences", "entities", "events", "cases", "patterns"},
			},
			"key":     map[string]any{"type": "string", "pattern": `^[a-zA-Z0-9_\-\.]+$`},
			"content": map[string]any{"type": "string"},
		},
		"required":             []string{"category", "key", "content"},
		"additionalProperties": false,
	},
}

// --- notebook_read ---

var NotebookReadAgentSpec = tools.AgentToolSpec{
	Name:        "notebook_read",
	Version:     "1",
	Description: "read the current notebook snapshot or one notebook entry by URI",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var NotebookReadLlmSpec = llm.ToolSpec{
	Name:        "notebook_read",
	Description: stringPtr(sharedtoolmeta.Must("notebook_read").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	},
}

// --- notebook_write ---

var NotebookWriteAgentSpec = tools.AgentToolSpec{
	Name:        "notebook_write",
	Version:     "1",
	Description: "store a long-lived notebook entry that is always injected into context",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var NotebookWriteLlmSpec = llm.ToolSpec{
	Name:        "notebook_write",
	Description: stringPtr(sharedtoolmeta.Must("notebook_write").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"category": map[string]any{
				"type": "string",
				"enum": []string{"profile", "preferences", "entities", "events", "cases", "patterns", "general"},
			},
			"key":     map[string]any{"type": "string", "pattern": `^[a-zA-Z0-9_\-\.]+$`},
			"content": map[string]any{"type": "string"},
		},
		"required":             []string{"category", "key", "content"},
		"additionalProperties": false,
	},
}

// --- notebook_edit ---

var NotebookEditAgentSpec = tools.AgentToolSpec{
	Name:        "notebook_edit",
	Version:     "1",
	Description: "edit one notebook entry by URI",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var NotebookEditLlmSpec = llm.ToolSpec{
	Name:        "notebook_edit",
	Description: stringPtr(sharedtoolmeta.Must("notebook_edit").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri": map[string]any{"type": "string"},
			"category": map[string]any{
				"type": "string",
				"enum": []string{"profile", "preferences", "entities", "events", "cases", "patterns", "general"},
			},
			"key":     map[string]any{"type": "string", "pattern": `^[a-zA-Z0-9_\-\.]+$`},
			"content": map[string]any{"type": "string"},
		},
		"required":             []string{"uri", "category", "key", "content"},
		"additionalProperties": false,
	},
}

// --- notebook_forget ---

var NotebookForgetAgentSpec = tools.AgentToolSpec{
	Name:        "notebook_forget",
	Version:     "1",
	Description: "remove one notebook entry by URI",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var NotebookForgetLlmSpec = llm.ToolSpec{
	Name:        "notebook_forget",
	Description: stringPtr(sharedtoolmeta.Must("notebook_forget").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri": map[string]any{"type": "string"},
		},
		"required":             []string{"uri"},
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
	Description: stringPtr(sharedtoolmeta.Must("memory_forget").LLMDescription),
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
	return append(NotebookAgentSpecs(), MemoryAgentSpecs()...)
}

func NotebookAgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		NotebookReadAgentSpec,
		NotebookWriteAgentSpec,
		NotebookEditAgentSpec,
		NotebookForgetAgentSpec,
	}
}

func MemoryAgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		SearchAgentSpec,
		ReadAgentSpec,
		WriteAgentSpec,
		ForgetAgentSpec,
	}
}

// LlmSpecs 返回全部 memory tool 的 LlmToolSpec。
func LlmSpecs() []llm.ToolSpec {
	return append(NotebookLlmSpecs(), MemoryLlmSpecs()...)
}

func NotebookLlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		NotebookReadLlmSpec,
		NotebookWriteLlmSpec,
		NotebookEditLlmSpec,
		NotebookForgetLlmSpec,
	}
}

func MemoryLlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		SearchLlmSpec,
		ReadLlmSpec,
		WriteLlmSpec,
		ForgetLlmSpec,
	}
}
