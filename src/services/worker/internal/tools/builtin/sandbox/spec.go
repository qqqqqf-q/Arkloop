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
	ExecCommandSpec = tools.AgentToolSpec{
		Name:        "exec_command",
		Version:     "1",
		Description: "run a command in the default persistent shell session inside the isolated sandbox",
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

var ExecCommandLlmSpec = llm.ToolSpec{
	Name:        "exec_command",
	Description: stringPtr("run a command in the default persistent shell session inside the isolated sandbox. The session is reused automatically inside the same run, so do not pass session_id. Use this tool to start shell work. For long-running commands, call exec_command first, then keep using write_stdin with the returned session_id. Write files meant for the final answer to /tmp/output/ so they appear in artifacts."),
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
	Description: stringPtr("send stdin to, or poll output from, a running shell session. Pass the session_id returned by exec_command. Set chars to a non-empty string to write stdin. Set chars to an empty string, or omit it, to poll for new output without repeating already delivered output."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "session id returned by exec_command",
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
		"required":             []string{"session_id"},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		PythonExecuteSpec,
		ExecCommandSpec,
		WriteStdinSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		PythonExecuteLlmSpec,
		ExecCommandLlmSpec,
		WriteStdinLlmSpec,
	}
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
