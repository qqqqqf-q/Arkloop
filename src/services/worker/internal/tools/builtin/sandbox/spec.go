package sandbox

import (
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var (
	CodeExecuteSpec = tools.AgentToolSpec{
		Name:        "code_execute",
		Version:     "1",
		Description: "execute Python code in isolated sandbox and return output",
		RiskLevel:   tools.RiskLevelHigh,
		SideEffects: true,
	}
	ShellExecuteSpec = tools.AgentToolSpec{
		Name:        "shell_execute",
		Version:     "1",
		Description: "execute shell commands in isolated sandbox and return output",
		RiskLevel:   tools.RiskLevelHigh,
		SideEffects: true,
	}
)

var CodeExecuteLlmSpec = llm.ToolSpec{
	Name:        "code_execute",
	Description: stringPtr("execute Python code in an isolated sandbox environment. Pre-installed libraries include numpy, pandas, matplotlib, plotly, scipy, sympy, pillow, scikit-learn, etc. For charts and visualizations, prefer Plotly (plotly.express or plotly.graph_objects) over matplotlib. Use fig.write_image() for PNG output (kaleido is pre-installed). Only fall back to fig.write_html() if write_image fails. Do not set pio.renderers or attempt to open a browser. To produce output files (images, CSVs, HTML, etc.), write them to /tmp/output/. Files there are automatically uploaded; the tool result includes an artifacts array with each file's key, filename, size and mime_type. To display an artifact in your response, reference it using the key from the artifacts array: use ![alt](artifact:<key>) for images/SVG, or [label](artifact:<key>) for other files. NEVER use /tmp/output/ paths as links."),
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
	Description: stringPtr("execute shell commands in an isolated sandbox environment"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":    map[string]any{"type": "string"},
			"timeout_ms": map[string]any{"type": "integer", "minimum": 1000, "maximum": 300000},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		CodeExecuteSpec,
		ShellExecuteSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		CodeExecuteLlmSpec,
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
