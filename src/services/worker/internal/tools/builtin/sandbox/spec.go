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
	BrowserSpec = tools.AgentToolSpec{
		Name:        "browser",
		Version:     "1",
		Description: "execute browser automation commands in an isolated browser sandbox with serialized session semantics",
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
			"share_scope": map[string]any{
				"type":        "string",
				"enum":        []string{"run", "thread", "workspace", "org"},
				"description": "share scope used only when a new session must be created",
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

var BrowserLlmSpec = llm.ToolSpec{
	Name:        "browser",
	Description: llmStringPtr(sharedtoolmeta.Must("browser").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "raw agent-browser CLI subcommand to execute, such as navigate <url>, snapshot, screenshot, click <ref>, or type <ref> <text>; for actions that may need the page to settle, prefer a realistic yield_time_ms instead of tiny values",
			},
			"session_ref": map[string]any{
				"type":        "string",
				"description": "stable browser session reference; omit to reuse the default browser session. This is not a mode flag, so do not pass placeholder values such as new, resume, or fork, and do not emulate modes with extra fields like session_mode or share_scope",
			},
			"yield_time_ms": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     30000,
				"description": "time to wait for browser command output before returning; increase this for navigation, snapshot after navigation, and render-heavy interactions to reduce premature running=true responses",
			},
		},
		"required":             []string{"command"},
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
