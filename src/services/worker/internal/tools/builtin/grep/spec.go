package grep

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "grep",
	Version:     "1",
	Description: "search file contents by regex pattern",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "grep",
	Description: strPtr(sharedtoolmeta.Must("grep").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "regex pattern to search for in file contents",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "search root directory (default: working directory)",
			},
			"include": map[string]any{
				"type":        "string",
				"description": "file glob to restrict search (e.g. *.go, *.ts)",
			},
			"context_lines": map[string]any{
				"type":        "integer",
				"description": "number of context lines before and after each match (0-10). When omitted and output_mode=content, auto-context is applied based on match count",
			},
			"output_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"content", "files_with_matches", "count"},
				"description": "output mode: files_with_matches (default, file paths only), content (matching lines with context), count (match counts per file)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "max results to return (default 200, max 1000). Use with offset for pagination",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "number of results to skip for pagination (default 0)",
			},
			"case_sensitive": map[string]any{
				"type":        "boolean",
				"description": "if false, perform case-insensitive matching",
			},
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "if true, allow patterns to match across line boundaries",
			},
			"file_type": map[string]any{
				"type":        "string",
				"description": "comma-separated file types to search (e.g. 'go,ts')",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
