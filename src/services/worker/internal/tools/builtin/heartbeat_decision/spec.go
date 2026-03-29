package heartbeat_decision

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "heartbeat_decision"

// AgentSpec 用于 ToolRegistry 注册（ToolBuild → Bind 引用）。
var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "Use only in heartbeat runs: declare intent to reply or stay silent, and persist memory fragments.",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

// Spec 是 heartbeat_decision 工具的 LLM schema 定义。
var Spec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("Use only in heartbeat runs: declare intent to reply or stay silent, and persist memory fragments."),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"reply"},
		"properties": map[string]any{
			"reply": map[string]any{
				"type":        "boolean",
				"description": "true = 本次心跳有内容发送给用户；false = 静默，run 立即结束。",
			},
			"memory_fragments": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
				"description": "需要持久化到长期记忆的简短事实性笔记。" +
					"只在有实质性内容需要记住时才填写。",
			},
		},
	},
}

func strPtr(s string) *string { return &s }

// SystemProtocolSnippet 返回注入 system prompt 的心跳协议说明。
func SystemProtocolSnippet() string {
	return "You are in an LLM heartbeat turn. " +
		"You MUST call `" + ToolName + "` FIRST, before any text output, to declare your intent. " +
		"If nothing needs user attention, call `" + ToolName + "` with reply=false and output NO text — the run ends immediately. " +
		"If you want to send a message to the user, call `" + ToolName + "` with reply=true, then write the message body after the tool call. " +
		"If `" + ToolName + "` is never called, all text output is discarded. " +
		"Do not call other tools in the same final step as `" + ToolName + "`. " +
		"If this turn surfaced facts worth remembering long-term (user preferences, key decisions, follow-up items), " +
		"include them as brief notes in memory_fragments. " +
		"Call `" + ToolName + "` exactly once."
}
