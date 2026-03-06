package sandbox

import (
	"strings"

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
	ShellExecuteSpec = tools.AgentToolSpec{
		Name:        "shell_execute",
		Version:     "1",
		Description: "interact with a persistent shell session in isolated sandbox",
		RiskLevel:   tools.RiskLevelHigh,
		SideEffects: true,
	}
)

var PythonExecuteLlmSpec = llm.ToolSpec{
	Name:        "python_execute",
	Description: stringPtr("execute Python code in an isolated sandbox environment. Use this tool for any numerical calculations or data processing instead of computing manually. Pre-installed libraries include numpy, pandas, matplotlib, plotly, scipy, sympy, pillow, scikit-learn, etc. For charts and visualizations, prefer Plotly (plotly.express or plotly.graph_objects) over matplotlib. Use fig.write_image() for PNG output (kaleido is pre-installed). Only fall back to fig.write_html() if write_image fails. Do not set pio.renderers or attempt to open a browser. To produce output files (images, CSVs, HTML, etc.), write them to /tmp/output/. Files there are automatically uploaded; the tool result includes an artifacts array with each file's key, filename, size and mime_type. To display an artifact in your response, reference it using the key from the artifacts array: use ![alt](artifact:<key>) for images/SVG, or [label](artifact:<key>) for other files. NEVER use /tmp/output/ paths as links."),
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

var ShellExecuteLlmSpec = llm.ToolSpec{
	Name:        "shell_execute",
	Description: stringPtr("use the default interactive shell session in the sandbox. The session is reused automatically inside the same run, so do not pass session_id. Use action=open to attach, action=exec to run a command, action=read to fetch more output from cursor, action=write to send stdin to the current foreground process, action=signal to interrupt long-running work, and action=close when the shell is no longer needed. For long-running commands, call exec first and then keep calling read with the returned cursor until running becomes false. Write files meant for the final answer to /tmp/output/ so they appear in artifacts."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"open", "exec", "read", "write", "signal", "close"},
				"description": "shell action to perform",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "required when action=exec",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "optional working directory for open or exec",
			},
			"cursor": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "output cursor used by read",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "stdin payload when action=write",
			},
			"signal": map[string]any{
				"type":        "string",
				"enum":        []string{"SIGINT", "SIGTERM", "SIGKILL"},
				"description": "signal sent by action=signal; defaults to SIGINT",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"minimum":     1000,
				"maximum":     300000,
				"description": "command timeout for exec",
			},
			"yield_time_ms": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     30000,
				"description": "time to wait for incremental output before returning",
			},
		},
		"required": []string{"action"},
		"allOf": []any{
			map[string]any{
				"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"const": "exec"}}},
				"then": map[string]any{"required": []string{"command"}},
			},
			map[string]any{
				"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"const": "write"}}},
				"then": map[string]any{"required": []string{"input"}},
			},
		},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		PythonExecuteSpec,
		ShellExecuteSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		PythonExecuteLlmSpec,
		ShellExecuteLlmSpec,
	}
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
