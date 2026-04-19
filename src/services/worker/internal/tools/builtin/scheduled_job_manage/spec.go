package scheduled_job_manage

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "scheduled_job_manage"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "Manage scheduled jobs: list, get, create, update, delete, run, status, runs, wake.",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var Spec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("Manage scheduled jobs: list, get, create, update, delete, run, status, runs, wake."),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "get", "create", "update", "delete", "run", "status", "runs", "wake"},
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
				"enum":        []string{"interval", "daily", "monthly", "weekdays", "weekly", "at", "cron"},
				"description": "Schedule type",
			},
			"fire_at": map[string]any{
				"type":        "string",
				"description": "ISO8601 datetime for one-time 'at' schedule (e.g. 2025-01-15T09:00:00Z)",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Standard 5-field cron expression (e.g. '*/5 * * * *')",
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
			"model": map[string]any{
				"type":        "string",
				"description": "Model override (provider^model)",
			},
			"persona_key": map[string]any{
				"type":        "string",
				"description": "Persona key for this job",
			},
			"work_dir": map[string]any{
				"type":        "string",
				"description": "Working directory",
			},
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable or disable the job",
			},
			"delete_after_run": map[string]any{
				"type":        "boolean",
				"description": "Delete job after it fires once (useful for 'at' one-time jobs)",
			},
			"thinking": map[string]any{
				"type":        "boolean",
				"description": "Enable extended thinking for this job's run",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Run timeout in seconds (0 means default)",
			},
			"light_context": map[string]any{
				"type":        "boolean",
				"description": "Use lightweight context injection for this job's run",
			},
			"tools_allow": map[string]any{
				"type":        "string",
				"description": "Comma-separated list of allowed tool names, empty means all tools allowed",
			},
		},
	},
}

func strPtr(s string) *string { return &s }
