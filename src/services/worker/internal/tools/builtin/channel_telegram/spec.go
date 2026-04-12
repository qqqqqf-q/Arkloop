package channel_telegram

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	ToolReact    = "telegram_react"
	ToolReply    = "telegram_reply"
	ToolSendFile = "telegram_send_file"
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
	Version:     "3",
	Description: "Set the reply-to reference for the current Telegram conversation output",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
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
		"Set the reply-to reference for your output in the current Telegram conversation. " +
			"Telegram runs do not attach reply references automatically. " +
			"Call this tool first with the target message_id, then write your reply text normally — " +
			"the system will deliver your text as a reply to that message. " +
			"In group triggers such as @mention, keyword, or reply, use this tool whenever you want the outbound message to stay attached to a specific message.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reply_to_message_id": map[string]any{
				"type":        "string",
				"description": "Telegram message_id to reply to (decimal string)",
			},
		},
		"required":             []string{"reply_to_message_id"},
		"additionalProperties": false,
	},
}

var SendFileAgentSpec = tools.AgentToolSpec{
	Name:        ToolSendFile,
	Version:     "1",
	Description: "Send a media file (photo, document, audio, video, voice, animation) to the current Telegram chat",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var SendFileLlmSpec = llm.ToolSpec{
	Name: ToolSendFile,
	Description: sp(
		"Send a media file to the current Telegram chat. The file_url can be either a publicly accessible URL (S3, CDN) OR a local file path on the bot's filesystem (e.g., /Users/x/Downloads/file.pdf). " +
			"Use kind to specify the media type: photo, document, audio, video, voice, or animation.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_url": map[string]any{
				"type":        "string",
				"description": "URL to the file (https://...) or local file path (/Users/x/Downloads/...)",
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{"photo", "document", "audio", "video", "voice", "animation"},
				"description": "Media type: photo (images), document (files), audio (music/podcasts), video (video clips), voice (voice notes), animation (GIFs)",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional caption text (supports Telegram HTML formatting)",
			},
		},
		"required":             []string{"file_url", "kind"},
		"additionalProperties": false,
	},
}
