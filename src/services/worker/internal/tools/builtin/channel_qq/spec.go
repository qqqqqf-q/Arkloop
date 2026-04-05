package channel_qq

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	ToolReact    = "qq_react"
	ToolReply    = "qq_reply"
	ToolSendFile = "qq_send_file"
)

func sp(s string) *string { return &s }

var ReactAgentSpec = tools.AgentToolSpec{
	Name:        ToolReact,
	Version:     "1",
	Description: "Set an emoji reaction on a QQ message (NapCat set_msg_emoji_like)",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var ReplyAgentSpec = tools.AgentToolSpec{
	Name:        ToolReply,
	Version:     "1",
	Description: "Set the reply-to reference for the current QQ conversation output",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var SendFileAgentSpec = tools.AgentToolSpec{
	Name:        ToolSendFile,
	Version:     "1",
	Description: "Send a media file (image, record, video) to the current QQ chat",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var ReactLlmSpec = llm.ToolSpec{
	Name: ToolReact,
	Description: sp(
		"Add an emoji reaction to a QQ message using NapCat's set_msg_emoji_like API. " +
			"emoji_id is the QQ face ID (string). " +
			"Optional message_id targets that message; omit to react to the inbound user message.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"emoji_id": map[string]any{
				"type":        "string",
				"description": "QQ face emoji ID (e.g. \"76\" for thumbs up)",
			},
			"message_id": map[string]any{
				"type":        "string",
				"description": "Message ID to react to; omit to react to the inbound message",
			},
		},
		"required":             []string{"emoji_id"},
		"additionalProperties": false,
	},
}

var ReplyLlmSpec = llm.ToolSpec{
	Name: ToolReply,
	Description: sp(
		"Set the reply-to reference for your output in the current QQ conversation. " +
			"Call this tool with the target message_id, then write your reply text normally. " +
			"Only use when you need to quote a specific message; for normal replies the system handles references automatically.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reply_to_message_id": map[string]any{
				"type":        "string",
				"description": "Message ID to reply to",
			},
		},
		"required":             []string{"reply_to_message_id"},
		"additionalProperties": false,
	},
}

var SendFileLlmSpec = llm.ToolSpec{
	Name: ToolSendFile,
	Description: sp(
		"Send a media file to the current QQ chat via OneBot segment. " +
			"file_url can be a publicly accessible URL or a local file path. " +
			"Use kind to specify the media type: image, record (voice), or video.",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_url": map[string]any{
				"type":        "string",
				"description": "URL or local path to the file",
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{"image", "record", "video"},
				"description": "Media type: image, record (voice), video",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional text sent along with the media",
			},
		},
		"required":             []string{"file_url", "kind"},
		"additionalProperties": false,
	},
}
