package telegram_heartbeat_command

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "telegram_heartbeat_command"

// AgentSpec 用于 ToolRegistry 注册（已废弃 LLM tool 方案，保留供编译）。
var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "Manage heartbeat settings for the current Telegram channel",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

// Spec 是 LLM 工具 schema 定义（已废弃 LLM tool 方案，保留供编译）。
var Spec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("Manage heartbeat settings for the current Telegram channel"),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"status", "on", "off", "interval", "model"},
			},
			"value": map[string]any{
				"type": "string",
			},
		},
	},
}

func strPtr(s string) *string { return &s }
