package accountapi

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

type InboundMessage struct {
	ChannelID         uuid.UUID
	ChannelType       string
	PlatformChatID    string
	PlatformMsgID     string
	PlatformUserID    string
	PlatformUsername  string
	ConversationType  string
	ConversationTitle string
	DateUnix          int64
	Text              string
	CommandText       string
	MentionsBot       bool
	IsReplyToBot      bool
	MatchesKeyword    bool
	ReplyToMsgID      *string
	ReplyToPreview    string
	QuoteText         string
	QuotePosition     *int
	QuoteIsManual     bool
	MessageThreadID   *string
	MediaAttachments  []InboundAttachment
	ForwardFromName   string
	RawPayload        json.RawMessage

	// ChatType is kept as a compatibility alias while channel adapters migrate
	// to ConversationType.
	ChatType string
}

type InboundAttachment struct {
	Type            string `json:"type"`
	FileID          string `json:"file_id,omitempty"`
	ThumbnailFileID string `json:"thumbnail_file_id,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	MimeType        string `json:"mime_type,omitempty"`
	URL             string `json:"url,omitempty"`
	Size            int64  `json:"size,omitempty"`
	Width           int64  `json:"width,omitempty"`
	Height          int64  `json:"height,omitempty"`
	DurationMs      int64  `json:"duration_ms,omitempty"`
	Caption         string `json:"caption,omitempty"`
}

type InboundConversation struct {
	ChannelID      uuid.UUID
	ChannelType    string
	PlatformChatID string
	ThreadID       *string
	IsPrivate      bool
}

func (m InboundMessage) IsPrivate() bool {
	ct := strings.TrimSpace(m.ConversationType)
	if ct == "" {
		ct = strings.TrimSpace(m.ChatType)
	}
	return strings.EqualFold(ct, "private") || strings.EqualFold(ct, "dm")
}

func (m InboundMessage) HasContent() bool {
	return strings.TrimSpace(m.Text) != "" || len(m.MediaAttachments) > 0
}

func (m InboundMessage) ShouldCreateRun() bool {
	return m.IsPrivate() || m.MentionsBot || m.IsReplyToBot || m.MatchesKeyword
}

func BuildChannelDeliveryPayload(incoming InboundMessage, identityID uuid.UUID) map[string]any {
	channelType := strings.TrimSpace(incoming.ChannelType)
	conversationType := strings.TrimSpace(incoming.ConversationType)
	if conversationType == "" {
		conversationType = strings.TrimSpace(incoming.ChatType)
	}
	payload := map[string]any{
		"channel_id":                 incoming.ChannelID.String(),
		"channel_type":               channelType,
		"platform_chat_id":           strings.TrimSpace(incoming.PlatformChatID),
		"platform_message_id":        strings.TrimSpace(incoming.PlatformMsgID),
		"sender_channel_identity_id": identityID.String(),
		"conversation_type":          conversationType,
		"mentions_bot":               incoming.MentionsBot,
		"is_reply_to_bot":            incoming.IsReplyToBot,
		"conversation_ref":           map[string]any{"target": strings.TrimSpace(incoming.PlatformChatID)},
		"inbound_message_ref":        map[string]any{"message_id": strings.TrimSpace(incoming.PlatformMsgID)},
		"trigger_message_ref":        map[string]any{"message_id": strings.TrimSpace(incoming.PlatformMsgID)},
	}
	if incoming.ReplyToMsgID != nil && strings.TrimSpace(*incoming.ReplyToMsgID) != "" {
		payload["inbound_reply_to_message_id"] = strings.TrimSpace(*incoming.ReplyToMsgID)
	}
	if incoming.MessageThreadID != nil && strings.TrimSpace(*incoming.MessageThreadID) != "" {
		threadID := strings.TrimSpace(*incoming.MessageThreadID)
		payload["message_thread_id"] = threadID
		payload["conversation_ref"].(map[string]any)["thread_id"] = threadID
	}
	return payload
}

func channelDispatchMode(channelType string) string {
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case "telegram", "discord":
		return "burst_v1"
	case "qq", "feishu", "weixin":
		return "immediate"
	default:
		return "immediate"
	}
}
