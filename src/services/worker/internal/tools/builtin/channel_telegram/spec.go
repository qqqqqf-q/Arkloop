package channel_telegram

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	ToolReact = "telegram_react"
	ToolReply = "telegram_reply"
)

func sp(s string) *string { return &s }

var ReactAgentSpec = tools.AgentToolSpec{
	Name:        ToolReact,
	Version:     "1",
	Description: "Set an emoji reaction on a Telegram message in the current channel conversation",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var ReplyAgentSpec = tools.AgentToolSpec{
	Name:        ToolReply,
	Version:     "1",
	Description: "Send an HTML-formatted reply in the current Telegram chat (common Markdown converted), optionally quoting a message",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var ReactLlmSpec = llm.ToolSpec{
	Name: ToolReact,
	Description: sp(
		"Set one emoji reaction on a Telegram message in the current channel run chat. " +
			"Optional message_id targets that message; omit it to react to the inbound user message that started this run. " +
			"Do not use reply_to_message_id here (that is only for telegram_reply).",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"emoji": map[string]any{
				"type":        "string",
				"description": "Single emoji or Telegram-supported reaction; provide emoji or reaction (exactly one non-empty)",
			},
			"reaction": map[string]any{
				"type":        "string",
				"description": "Alias of emoji; provide emoji or reaction (exactly one non-empty)",
			},
			"message_id": map[string]any{
				"type":        "string",
				"description": "Telegram message_id as decimal string; omit to use inbound message from context",
			},
		},
		// OpenAI tool parameters forbid top-level oneOf/anyOf; exclusivity validated in executor.
		"additionalProperties": false,
	},
}

var ReplyLlmSpec = llm.ToolSpec{
	Name: ToolReply,
	Description: sp(
		"Send a text reply in the current Telegram conversation: body uses common Markdown (**bold**, `code`, links) converted to Telegram HTML; reply_to_message_id is required.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "Message body: common Markdown → Telegram HTML (bold, code, fenced block, https links)",
			},
			"reply_to_message_id": map[string]any{
				"type":        "string",
				"description": "Telegram message_id to reply to (decimal string)",
			},
		},
		"required":             []string{"text", "reply_to_message_id"},
		"additionalProperties": false,
	},
}
