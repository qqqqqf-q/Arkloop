//go:build desktop

package localshell

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var ExecCommandAgentSpec = tools.AgentToolSpec{
	Name:        "exec_command",
	Version:     "1",
	Description: "run a command in a persistent local shell session",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var WriteStdinAgentSpec = tools.AgentToolSpec{
	Name:        "write_stdin",
	Version:     "1",
	Description: "send stdin to, or poll output from, a running local shell session",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var ExecCommandLlmSpec = llm.ToolSpec{
	Name:        "exec_command",
	Description: strPtr("Execute a shell command on the local machine. The shell session persists across calls within the same conversation. Use this to run terminal commands, install packages, manage files, run scripts, etc."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "command to execute",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "optional working directory for the command",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"minimum":     1000,
				"maximum":     300000,
				"description": "command timeout in milliseconds",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	},
}

var WriteStdinLlmSpec = llm.ToolSpec{
	Name:        "write_stdin",
	Description: strPtr("Send input to the running shell session, or poll for new output. Use this when a command is waiting for interactive input, or to check the latest output of a long-running command."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chars": map[string]any{
				"type":        "string",
				"description": "stdin payload; omit or set empty string to poll",
			},
			"yield_time_ms": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     30000,
				"description": "time to wait for new output before returning",
			},
		},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{ExecCommandAgentSpec, WriteStdinAgentSpec}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{ExecCommandLlmSpec, WriteStdinLlmSpec}
}

func strPtr(s string) *string { return &s }
