package readfile

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "read_file",
	Version:     "1",
	Description: "read file content with line numbers and optional range",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "read_file",
	Description: strPtr("legacy file read tool; use read with source.kind=file_path"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "path to the file to read (relative to working directory or absolute)",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "line number to start from (1-based, default 1)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "maximum number of lines to read (default 2000)",
			},
		},
		"required":             []string{"file_path"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
