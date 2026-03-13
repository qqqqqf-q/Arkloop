package acptool

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "code_agent",
	Version:     "1",
	Description: "delegate coding task to acp-compatible agent in sandbox",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "code_agent",
	Description: stringPtr("Delegate a code-heavy task to a specialized coding agent running in sandbox. Use this when the task requires extensive file reading, code writing, testing, or debugging that would benefit from a dedicated coding agent with its own context and tool chain. The agent operates autonomously within the sandbox workspace."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Clear description of the coding task to delegate. Be specific about what files to modify, what tests to run, and what the expected outcome should be.",
			},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	},
}

func stringPtr(s string) *string { return &s }
