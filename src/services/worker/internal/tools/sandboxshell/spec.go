//go:build desktop

package sandboxshell

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var ExecCommandAgentSpec = tools.AgentToolSpec{
	Name:        "exec_command",
	Version:     "1",
	Description: "run a command in an isolated VM shell session",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var WriteStdinAgentSpec = tools.AgentToolSpec{
	Name:        "write_stdin",
	Version:     "1",
	Description: "send stdin to, or poll output from, a running VM shell session",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var ExecCommandLlmSpec = llm.ToolSpec{
	Name:        "exec_command",
	Description: strPtr("Execute a shell command in an isolated virtual machine. The shell session persists across calls within the same conversation. Prefer the cwd parameter for directory changes instead of prefixing commands with cd &&. Use this to run terminal commands, install packages, manage files, run scripts, etc."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "command to execute; keep the command body focused and prefer cwd for directory changes instead of prefixing cd &&",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "optional working directory for the command; prefer this over embedding cd ... && inside command",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"minimum":     1000,
				"maximum":     300000,
				"description": "command timeout in milliseconds",
			},
			"env": map[string]any{
				"type":                 "object",
				"description":         "environment variable overrides for the command",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	},
}

var WriteStdinLlmSpec = llm.ToolSpec{
	Name:        "write_stdin",
	Description: strPtr("Send input to the running VM shell session, or poll for new output. Use this when a command is waiting for interactive input, or to check the latest output of a long-running command."),
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
