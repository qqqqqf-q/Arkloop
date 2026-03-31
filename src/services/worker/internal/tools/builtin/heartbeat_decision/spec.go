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
				"description": "true = send a reply to the user this heartbeat; false = silent, run ends immediately.",
			},
			"memory_fragments": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
				"description": "short factual notes to persist to long-term memory; only include when there is substantive content worth remembering.",
			},
		},
	},
}

func strPtr(s string) *string { return &s }

// SystemProtocolSnippet 返回注入 system prompt 的心跳协议说明。
func SystemProtocolSnippet() string {
	return "You are in an LLM heartbeat turn. " +
		"SYSTEM CONSTRAINT: calling `" + ToolName + "` with reply=true is the ONLY way any message reaches the user. " +
		"Text you write without calling the tool first is silently discarded by the runtime — the user never sees it. " +
		"There is no fallback path. " +
		"Step 1 — call `" + ToolName + "` exactly once, before writing any message text: " +
		"reply=false if you have nothing to say (run ends immediately, write no text); " +
		"reply=true if you want to send a message (run continues so you can write the message body). " +
		"Step 2 — only if reply=true: write the message body after the tool result. " +
		"Do not call any other tool in the same step as `" + ToolName + "`. " +
		"If this turn surfaced facts worth remembering long-term, include them in memory_fragments."
}
