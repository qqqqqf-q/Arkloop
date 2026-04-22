//go:build desktop

package localshell

import (
	"strings"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var ExecCommandAgentSpec = tools.AgentToolSpec{
	Name:        "exec_command",
	Version:     "1",
	Description: "run a command on desktop, either buffered or as an explicit interactive process",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var ContinueProcessAgentSpec = tools.AgentToolSpec{
	Name:        "continue_process",
	Version:     "1",
	Description: "continue a running desktop process by reading output and optionally sending stdin",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var TerminateProcessAgentSpec = tools.AgentToolSpec{
	Name:        "terminate_process",
	Version:     "1",
	Description: "terminate a running desktop process",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var ResizeProcessAgentSpec = tools.AgentToolSpec{
	Name:        "resize_process",
	Version:     "1",
	Description: "resize a running PTY desktop process",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var ExecCommandLlmSpec = llm.ToolSpec{
	Name:        "exec_command",
	Description: llmStringPtr(
		"run a shell command on the user's local machine. Default mode is buffered, which executes one command to completion with stdin closed. " +
			"Use follow for long-running output-only processes, stdin for non-PTY processes that need later input, and pty only for real terminal-style interaction. " +
			"The backend returns a process_ref only for follow/stdin/pty modes. Continue those processes with continue_process, terminate them with terminate_process, and resize only pty processes with resize_process. " +
			"When you only need to change directories, prefer the cwd parameter instead of prefixing the command with cd &&. " +
			"Do not use for file operations — use read/write_file/edit/grep instead.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"buffered", "follow", "stdin", "pty"},
				"description": "buffered runs one command to completion; follow keeps reading output; stdin allows later input without a PTY; pty opens a real terminal session",
			},
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
				"maximum":     1800000,
				"description": "command timeout; required for follow, stdin, and pty modes",
			},
			"size": map[string]any{
				"type":        "object",
				"description": "initial terminal size; only valid for mode=pty",
				"properties": map[string]any{
					"rows": map[string]any{"type": "integer", "minimum": 1},
					"cols": map[string]any{"type": "integer", "minimum": 1},
				},
				"required":             []string{"rows", "cols"},
				"additionalProperties": false,
			},
			"env": map[string]any{
				"type":                 "object",
				"description":          "environment variable overrides for the command; values may be strings or null to unset",
				"additionalProperties": map[string]any{"type": []string{"string", "null"}},
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	},
}

var ContinueProcessLlmSpec = llm.ToolSpec{
	Name:        "continue_process",
	Description: llmStringPtr(sharedtoolmeta.Must("continue_process").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"process_ref": map[string]any{
				"type":        "string",
				"description": "process reference returned by exec_command in follow, stdin, or pty mode",
			},
			"cursor": map[string]any{
				"type":        "string",
				"description": "read output strictly after this cursor; pass the previous next_cursor value",
			},
			"wait_ms": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     30000,
				"description": "time to wait for new output before returning",
			},
			"stdin_text": map[string]any{"type": "string"},
			"input_seq": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "required when stdin_text is provided; use a stable positive integer to deduplicate retries",
			},
			"close_stdin": map[string]any{"type": "boolean"},
		},
		"required":             []string{"process_ref", "cursor"},
		"additionalProperties": false,
	},
}

var TerminateProcessLlmSpec = llm.ToolSpec{
	Name:        "terminate_process",
	Description: llmStringPtr(sharedtoolmeta.Must("terminate_process").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"process_ref": map[string]any{"type": "string"},
		},
		"required":             []string{"process_ref"},
		"additionalProperties": false,
	},
}

var ResizeProcessLlmSpec = llm.ToolSpec{
	Name:        "resize_process",
	Description: llmStringPtr(sharedtoolmeta.Must("resize_process").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"process_ref": map[string]any{"type": "string"},
			"rows":        map[string]any{"type": "integer", "minimum": 1},
			"cols":        map[string]any{"type": "integer", "minimum": 1},
		},
		"required":             []string{"process_ref", "rows", "cols"},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		ExecCommandAgentSpec,
		ContinueProcessAgentSpec,
		TerminateProcessAgentSpec,
		ResizeProcessAgentSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		ExecCommandLlmSpec,
		ContinueProcessLlmSpec,
		TerminateProcessLlmSpec,
		ResizeProcessLlmSpec,
	}
}

func llmStringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
