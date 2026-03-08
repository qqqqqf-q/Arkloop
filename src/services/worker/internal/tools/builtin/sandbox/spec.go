package sandbox

import (
	"strings"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var (
	PythonExecuteSpec = tools.AgentToolSpec{
		Name:        "python_execute",
		Version:     "1",
		Description: "execute Python code in isolated sandbox and return output",
		RiskLevel:   tools.RiskLevelHigh,
		SideEffects: true,
	}
	ExecCommandSpec = tools.AgentToolSpec{
		Name:        "exec_command",
		Version:     "1",
		Description: "run a command in a persistent shell session inside the isolated sandbox",
		RiskLevel:   tools.RiskLevelHigh,
		SideEffects: true,
	}
	WriteStdinSpec = tools.AgentToolSpec{
		Name:        "write_stdin",
		Version:     "1",
		Description: "send stdin to, or poll output from, a running shell session inside the isolated sandbox",
		RiskLevel:   tools.RiskLevelHigh,
		SideEffects: true,
	}
)

var PythonExecuteLlmSpec = llm.ToolSpec{
	Name:        "python_execute",
	Description: llmStringPtr(sharedtoolmeta.Must("python_execute").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code":       map[string]any{"type": "string"},
			"timeout_ms": map[string]any{"type": "integer", "minimum": 1000, "maximum": 300000},
		},
		"required":             []string{"code"},
		"additionalProperties": false,
	},
}

var ExecCommandLlmSpec = llm.ToolSpec{
	Name:        "exec_command",
	Description: llmStringPtr(sharedtoolmeta.Must("exec_command").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"auto", "new", "resume", "fork"},
				"description": "how to resolve the target shell session",
			},
			"session_ref": map[string]any{
				"type":        "string",
				"description": "stable session reference used for resume or explicit attach",
			},
			"from_session_ref": map[string]any{
				"type":        "string",
				"description": "source session reference when session_mode is fork",
			},
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
				"description": "command timeout",
			},
			"yield_time_ms": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     30000,
				"description": "time to wait for incremental output before returning",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	},
}

var WriteStdinLlmSpec = llm.ToolSpec{
	Name:        "write_stdin",
	Description: llmStringPtr(sharedtoolmeta.Must("write_stdin").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_ref": map[string]any{
				"type":        "string",
				"description": "stable session reference returned by exec_command",
			},
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
		"required":             []string{"session_ref"},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{PythonExecuteSpec, ExecCommandSpec, WriteStdinSpec}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{PythonExecuteLlmSpec, ExecCommandLlmSpec, WriteStdinLlmSpec}
}

func llmStringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
