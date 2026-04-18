package scheduled_job_manage

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "scheduled_job_manage"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "Manage scheduled jobs: list, get, create, update, delete.",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var Spec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("Manage scheduled jobs: list, get, create, update, delete."),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "get", "create", "update", "delete"},
				"description": "Operation type",
			},
			"job_id": map[string]any{
				"type":        "string",
				"description": "Job ID (required for get/update/delete)",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Job name",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Job description",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Execution prompt",
			},
			"thread_id": map[string]any{
				"type":        "string",
				"description": "Thread ID to reuse",
			},
			"schedule_kind": map[string]any{
				"type":        "string",
				"enum":        []string{"interval", "daily", "monthly", "weekdays", "weekly"},
				"description": "Schedule type",
			},
			"interval_min": map[string]any{
				"type":        "integer",
				"description": "Interval in minutes (for interval kind)",
			},
			"daily_time": map[string]any{
				"type":        "string",
				"description": "HH:MM (for daily/weekdays/weekly kind)",
			},
			"monthly_day": map[string]any{
				"type":        "integer",
				"description": "Day of month 1-28 (for monthly kind)",
			},
			"monthly_time": map[string]any{
				"type":        "string",
				"description": "HH:MM (for monthly kind)",
			},
			"weekly_day": map[string]any{
				"type":        "integer",
				"description": "Day of week 0-6, where 0=Sunday (for weekly kind)",
			},
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone, e.g. Asia/Shanghai",
			},
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable or disable the job",
			},
		},
	},
}

func strPtr(s string) *string { return &s }
