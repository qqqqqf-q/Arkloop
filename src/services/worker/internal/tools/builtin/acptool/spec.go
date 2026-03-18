package acptool

import (
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
	Description: stringPtr("Delegate a task to an ACP-compatible coding agent. The agent operates autonomously with its own LLM, tools and workspace access. Suitable for code-heavy tasks that require extensive file reading, code writing, testing or debugging."),
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

func stringPtr(s string) *string { return &s }
