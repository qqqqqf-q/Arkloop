package acptool

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "acp_agent",
	Version:     "1",
	Description: "delegate task to an ACP-compatible agent in sandbox",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "acp_agent",
	Description: stringPtr("Delegate a task to an ACP-compatible agent running in sandbox. The agent operates autonomously with its own LLM, tools and workspace access. Suitable for code-heavy tasks that require extensive file reading, code writing, testing or debugging."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Clear description of the task to delegate. Be specific about what to do and what the expected outcome should be.",
			},
			"agent": map[string]any{
				"type":        "string",
				"description": "ACP agent to use. Defaults to opencode.",
				"default":     "opencode",
			},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	},
}

func stringPtr(s string) *string { return &s }
