package imagegenerate

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "image_generate"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "generate an image and save it as an artifact",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr(sharedtoolmeta.Must(ToolName).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "the full image generation prompt",
			},
			"input_images": map[string]any{
				"type":        "array",
				"description": "optional source images as artifact references, for example artifact:<key>",
				"items": map[string]any{
					"type": "string",
				},
			},
			"size": map[string]any{
				"type":        "string",
				"description": "optional output size, for example 1024x1024",
			},
			"quality": map[string]any{
				"type":        "string",
				"description": "optional output quality, for example low, medium, high, or auto",
			},
			"background": map[string]any{
				"type":        "string",
				"description": "optional background mode, for example transparent or opaque",
			},
			"output_format": map[string]any{
				"type":        "string",
				"description": "optional output image format, for example png, jpeg, or webp",
			},
		},
		"required":             []string{"prompt"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
