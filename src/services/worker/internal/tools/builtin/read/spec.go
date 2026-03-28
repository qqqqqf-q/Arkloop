package read

import (
	"fmt"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "read",
	Version:     "1",
	Description: "read file content or image attachments and return text output",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var AgentSpecMiniMax = tools.AgentToolSpec{
	Name:        ProviderNameMiniMax,
	LlmName:     "read",
	Version:     "1",
	Description: "read image attachments via minimax provider and return text output",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "read",
	Description: strPtr(sharedtoolmeta.Must("read").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type": "string",
						"enum": []string{"file_path", "message_attachment", "remote_url"},
					},
					"file_path": map[string]any{
						"type":        "string",
						"description": "path to read when source.kind=file_path",
					},
					"attachment_key": map[string]any{
						"type":        "string",
						"description": "message attachment key when source.kind=message_attachment",
					},
					"url": map[string]any{
						"type":        "string",
						"description": "remote http/https image URL when source.kind=remote_url",
					},
				},
				"required":             []string{"kind"},
				"additionalProperties": false,
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "required for image sources; what to analyze in the image",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "line number to start from (1-based, default 1) for file_path source",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "maximum number of lines to read (default 2000) for file_path source",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("maximum image size in bytes for image sources (default %d)", defaultMaxBytes),
				"default":     defaultMaxBytes,
				"minimum":     1,
				"maximum":     defaultMaxBytes,
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "optional timeout override in milliseconds for image sources",
				"minimum":     1,
				"maximum":     maxTimeoutMs,
			},
		},
		"required":             []string{"source"},
		"additionalProperties": false,
	},
}

func strPtr(value string) *string { return &value }
