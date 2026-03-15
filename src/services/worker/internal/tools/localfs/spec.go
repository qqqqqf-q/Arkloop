//go:build desktop

package localfs

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var FileReadAgentSpec = tools.AgentToolSpec{
	Name:        "file_read",
	Version:     "1",
	Description: "read a file from the local workspace",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var FileWriteAgentSpec = tools.AgentToolSpec{
	Name:        "file_write",
	Version:     "1",
	Description: "write or create a file in the local workspace",
	RiskLevel:   tools.RiskLevelHigh,
	SideEffects: true,
}

var FileReadLlmSpec = llm.ToolSpec{
	Name:        "file_read",
	Description: strPtr("Read the contents of a file from the local workspace. Returns the file content as text. Use relative paths from the workspace root."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "relative path to the file from the workspace root",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "line number to start reading from (1-based, optional)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "maximum number of lines to read (optional)",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	},
}

var FileWriteLlmSpec = llm.ToolSpec{
	Name:        "file_write",
	Description: strPtr("Write content to a file in the local workspace. Creates the file if it doesn't exist, or overwrites if it does. Parent directories are created automatically. Use relative paths from the workspace root."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "relative path to the file from the workspace root",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "content to write to the file",
			},
		},
		"required":             []string{"path", "content"},
		"additionalProperties": false,
	},
}

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{FileReadAgentSpec, FileWriteAgentSpec}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{FileReadLlmSpec, FileWriteLlmSpec}
}

func strPtr(s string) *string { return &s }
