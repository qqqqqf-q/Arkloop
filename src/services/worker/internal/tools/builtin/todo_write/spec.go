package todowrite

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "todo_write"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "manage a per-run or plan-bound todo list",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr(sharedtoolmeta.Must(ToolName).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"description": "Per-run todos. Use this mode when the todo list is not bound to a plan file.",
				"type":        "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"content":     map[string]any{"type": "string"},
						"active_form": map[string]any{"type": "string"},
						"status": map[string]any{
							"type": "string",
							"enum": []string{"pending", "in_progress", "completed", "cancelled"},
						},
					},
					"required":             []string{"id", "content", "status"},
					"additionalProperties": false,
				},
			},
			"plan_path": map[string]any{
				"type":        "string",
				"description": "Path to a .plan.md file. When executing an approved plan, use this for every plan todo status change. When provided, omit todos and update the plan file's front matter todos through updates.",
			},
			"updates": map[string]any{
				"type":        "array",
				"description": "Todo status updates for plan-bound mode.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"todo_id": map[string]any{
							"type":        "string",
							"description": "The id of the todo in the plan file.",
						},
						"status": map[string]any{
							"type": "string",
							"enum": []string{"pending", "in_progress", "completed", "cancelled"},
						},
					},
					"required":             []string{"todo_id", "status"},
					"additionalProperties": false,
				},
			},
		},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
