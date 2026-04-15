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
	Description: "Use only in heartbeat runs: declare intent to reply or stay silent.",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

// Spec 是 heartbeat_decision 工具的 LLM schema 定义。
var Spec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("Use only in heartbeat runs: declare intent to reply or stay silent."),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"reply"},
		"properties": map[string]any{
			"reply": map[string]any{
				"type":        "boolean",
				"description": "true = send a reply to the user this heartbeat; false = silent, run ends immediately.",
			},
		},
	},
}

func strPtr(s string) *string { return &s }

// SystemProtocolSnippet 返回注入 system prompt 的心跳协议说明。
func SystemProtocolSnippet() string {
	return "You are in an LLM heartbeat turn. This is a two-phase process:\n" +
		"Phase 1 (this turn): call `" + ToolName + "` exactly once. Do NOT output any text before the tool call — any text produced before the call is invisible to the user and wastes tokens.\n" +
		"  - reply=false → run ends immediately, no output.\n" +
		"  - reply=true → you proceed to Phase 2 with full tool access.\n" +
		"Phase 2 (after reply=true): all your tools are unlocked. " +
		"Use them freely to compose your reply to the user."
}
