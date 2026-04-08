package acptool

import (
	"arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/acp"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "acp_agent",
	Version:     "1",
	Description: "delegate task to an ACP-compatible coding agent",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "acp_agent",
	Description: stringPtr(toolmeta.Must("acp_agent").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Clear description of the task to delegate. Be specific about what to do and what the expected outcome should be.",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "ACP provider to use. Defaults to acp.opencode.",
				"default":     acp.DefaultProviderID,
			},
			"profile": map[string]any{
				"type":        "string",
				"description": "LLM profile for the agent. Controls which model the agent uses. Options: explore (fast/cheap), task (balanced, default), strong (best reasoning). If not set, the agent uses its own default configuration.",
				"enum":        []string{"explore", "task", "strong"},
			},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	},
}

var SpawnACPAgentSpec = tools.AgentToolSpec{
	Name:        "spawn_acp",
	Version:     "1",
	Description: "start an ACP coding agent asynchronously and return a handle",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var SpawnACPLlmSpec = llm.ToolSpec{
	Name:        "spawn_acp",
	Description: stringPtr(toolmeta.Must("spawn_acp").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Clear description of the task to delegate. Be specific about what to do and what the expected outcome should be.",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "ACP provider to use. Defaults to acp.opencode.",
				"default":     acp.DefaultProviderID,
			},
			"profile": map[string]any{
				"type":        "string",
				"description": "LLM profile for the agent. Options: explore (fast/cheap), task (balanced, default), strong (best reasoning).",
				"enum":        []string{"explore", "task", "strong"},
			},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	},
}

var SendACPAgentSpec = tools.AgentToolSpec{
	Name:        "send_acp",
	Version:     "1",
	Description: "send a follow-up prompt to an existing ACP agent session",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var SendACPLlmSpec = llm.ToolSpec{
	Name:        "send_acp",
	Description: stringPtr(toolmeta.Must("send_acp").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"handle_id": map[string]any{
				"type":        "string",
				"description": "Handle ID returned by spawn_acp.",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "The follow-up prompt to send to the agent.",
			},
		},
		"required":             []string{"handle_id", "input"},
		"additionalProperties": false,
	},
}

var WaitACPAgentSpec = tools.AgentToolSpec{
	Name:        "wait_acp",
	Version:     "1",
	Description: "block until a spawned ACP agent completes and return its output",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var WaitACPLlmSpec = llm.ToolSpec{
	Name:        "wait_acp",
	Description: stringPtr(toolmeta.Must("wait_acp").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"handle_id": map[string]any{
				"type":        "string",
				"description": "Handle ID returned by spawn_acp.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Maximum seconds to wait. Returns timeout=true if exceeded. Omit to use the default wait window.",
				"minimum":     1,
			},
		},
		"required":             []string{"handle_id"},
		"additionalProperties": false,
	},
}

var InterruptACPAgentSpec = tools.AgentToolSpec{
	Name:        "interrupt_acp",
	Version:     "1",
	Description: "cancel the current turn of a running ACP agent without closing the session",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var InterruptACPLlmSpec = llm.ToolSpec{
	Name:        "interrupt_acp",
	Description: stringPtr(toolmeta.Must("interrupt_acp").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"handle_id": map[string]any{
				"type":        "string",
				"description": "Handle ID returned by spawn_acp.",
			},
		},
		"required":             []string{"handle_id"},
		"additionalProperties": false,
	},
}

var CloseACPAgentSpec = tools.AgentToolSpec{
	Name:        "close_acp",
	Version:     "1",
	Description: "close an ACP agent session and terminate the underlying process",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var CloseACPLlmSpec = llm.ToolSpec{
	Name:        "close_acp",
	Description: stringPtr(toolmeta.Must("close_acp").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"handle_id": map[string]any{
				"type":        "string",
				"description": "Handle ID returned by spawn_acp.",
			},
		},
		"required":             []string{"handle_id"},
		"additionalProperties": false,
	},
}

func stringPtr(s string) *string { return &s }
