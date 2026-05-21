package activityrecorderfinish

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "activity_recorder_finish",
	Version:     "1",
	Description: "record the structured final outcome of an Activity Recorder Builder run",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "activity_recorder_finish",
	Description: strPtr("Call exactly once before ending an Activity Recorder Builder run. Use memory_write first for durable facts, then report the final status, checked sources, unavailable sources, and why no memory was written when applicable."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "final builder status",
				"enum":        []string{"memory_written", "no_durable_memory", "partial", "source_unavailable", "failed"},
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "short reason for the final status",
			},
			"sources_checked": map[string]any{
				"type":        "array",
				"description": "activity sources that were actually queried or inspected",
				"items": map[string]any{
					"type": "string",
				},
			},
			"sources_unavailable": map[string]any{
				"type":        "array",
				"description": "enabled or relevant sources that could not be queried",
				"items": map[string]any{
					"type": "string",
				},
			},
			"memory_write_count": map[string]any{
				"type":        "integer",
				"description": "number of memory_write calls made in this run",
				"minimum":     0,
			},
		},
		"required":             []string{"status", "reason", "sources_checked", "sources_unavailable", "memory_write_count"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
