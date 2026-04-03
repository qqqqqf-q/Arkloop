package pipeline

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ChannelContext struct {
	ChannelID               uuid.UUID
	ChannelType             string
	Conversation            ChannelConversationRef
	InboundMessage          ChannelMessageRef
	TriggerMessage          *ChannelMessageRef
	ConversationType        string
	MentionsBot             bool
	IsReplyToBot            bool
	SenderChannelIdentityID uuid.UUID
	SenderUserID            *uuid.UUID
	BotDisplayName          string
	BotUsername             string
}

func NewChannelContextMiddleware(pool *pgxpool.Pool) RunMiddleware {
	repo := data.ChannelDeliveryRepository{}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || len(rc.JobPayload) == 0 {
			return next(ctx, rc)
		}
		rawDelivery, ok := rc.JobPayload["channel_delivery"].(map[string]any)
		if !ok || len(rawDelivery) == 0 {
			return next(ctx, rc)
		}
		channelCtx, err := ParseChannelContextPayload(rawDelivery)
		if err != nil {
			return err
		}
		if pool != nil && channelCtx.SenderChannelIdentityID != uuid.Nil {
			identity, err := repo.GetIdentity(ctx, pool, channelCtx.SenderChannelIdentityID)
			if err != nil {
				return err
			}
			if identity != nil {
				channelCtx.SenderUserID = identity.UserID
			}
		}
		// channel 场景下 bot 的 memory 归属于 channel owner
		if pool != nil && channelCtx.SenderUserID == nil && channelCtx.ChannelID != uuid.Nil {
			ownerID, err := repo.GetChannelOwner(ctx, pool, channelCtx.ChannelID)
			if err != nil {
				return err
			}
			channelCtx.SenderUserID = ownerID
		}
		if pool != nil && channelCtx.ChannelID != uuid.Nil && channelCtx.ChannelType == "telegram" {
			configJSON, err := repo.GetChannelConfigJSON(ctx, pool, channelCtx.ChannelID)
			if err == nil && len(configJSON) > 0 {
				ux := ParseTelegramChannelUX(configJSON)
				channelCtx.BotDisplayName = ux.BotFirstName
				channelCtx.BotUsername = ux.BotUsername
			}
		}
		rc.ChannelContext = channelCtx
		rc.ChannelToolSurface = NewChannelToolSurfaceFromContext(channelCtx)
		if channelCtx.SenderUserID != nil {
			rc.UserID = channelCtx.SenderUserID
		}
		return next(ctx, rc)
	}
}

func ParseChannelContextPayload(payload map[string]any) (*ChannelContext, error) {
	return parseChannelContext(payload)
}

func parseChannelContext(payload map[string]any) (*ChannelContext, error) {
	channelID, err := requiredUUIDValue(payload, "channel_id")
	if err != nil {
		return nil, err
	}
	channelType, err := requiredStringValue(payload, "channel_type")
	if err != nil {
		return nil, err
	}
	senderIdentityID, err := requiredUUIDValue(payload, "sender_channel_identity_id")
	if err != nil {
		return nil, err
	}
	conversationRef, err := parseConversationRef(payload)
	if err != nil {
		return nil, err
	}
	inboundMessageRef, err := parseOptionalInboundMessageRef(payload)
	if err != nil {
		return nil, err
	}
	triggerMessageRef, err := parseOptionalMessageRef(payload, "trigger_message_ref", "reply_to_message_id")
	if err != nil {
		return nil, err
	}
	conversationType, _ := optionalStringValue(payload, "conversation_type")
	mentionsBot, _ := optionalBoolValue(payload, "mentions_bot")
	isReplyToBot, _ := optionalBoolValue(payload, "is_reply_to_bot")

	return &ChannelContext{
		ChannelID:               channelID,
		ChannelType:             channelType,
		Conversation:            conversationRef,
		InboundMessage:          inboundMessageRef,
		TriggerMessage:          triggerMessageRef,
		ConversationType:        conversationType,
		MentionsBot:             mentionsBot,
		IsReplyToBot:            isReplyToBot,
		SenderChannelIdentityID: senderIdentityID,
	}, nil
}

func parseConversationRef(payload map[string]any) (ChannelConversationRef, error) {
	if raw, ok := payload["conversation_ref"].(map[string]any); ok && len(raw) > 0 {
		target, err := requiredStringMapValue(raw, "target")
		if err != nil {
			return ChannelConversationRef{}, err
		}
		threadID, err := optionalStringMapValue(raw, "thread_id")
		if err != nil {
			return ChannelConversationRef{}, err
		}
		return ChannelConversationRef{Target: target, ThreadID: threadID}, nil
	}
	target, err := requiredStringValue(payload, "platform_chat_id")
	if err != nil {
		return ChannelConversationRef{}, err
	}
	threadID, err := optionalStringMapValue(payload, "message_thread_id")
	if err != nil {
		return ChannelConversationRef{}, err
	}
	return ChannelConversationRef{Target: target, ThreadID: threadID}, nil
}

func parseMessageRef(payload map[string]any, structuredKey string, fallbackKey string) (ChannelMessageRef, error) {
	if raw, ok := payload[structuredKey].(map[string]any); ok && len(raw) > 0 {
		messageID, err := requiredStringMapValue(raw, "message_id")
		if err != nil {
			return ChannelMessageRef{}, err
		}
		return ChannelMessageRef{MessageID: messageID}, nil
	}
	messageID, err := requiredStringValue(payload, fallbackKey)
	if err != nil {
		return ChannelMessageRef{}, err
	}
	return ChannelMessageRef{MessageID: messageID}, nil
}

func parseOptionalMessageRef(payload map[string]any, structuredKey string, fallbackKey string) (*ChannelMessageRef, error) {
	if raw, ok := payload[structuredKey]; ok && raw != nil {
		refMap, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", structuredKey)
		}
		ref, err := parseMessageRef(map[string]any{structuredKey: refMap}, structuredKey, "unused")
		if err != nil {
			return nil, err
		}
		return &ref, nil
	}
	if fallbackValue, ok := payload[fallbackKey]; ok && fallbackValue != nil {
		text, ok := fallbackValue.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", fallbackKey)
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil, nil
		}
		return &ChannelMessageRef{MessageID: trimmed}, nil
	}
	return nil, nil
}

// parseOptionalInboundMessageRef 解析入站消息引用，不存在时返回零值（heartbeat run 没有真实入站消息）。
func parseOptionalInboundMessageRef(payload map[string]any) (ChannelMessageRef, error) {
	if raw, ok := payload["inbound_message_ref"].(map[string]any); ok && len(raw) > 0 {
		messageID, err := requiredStringMapValue(raw, "message_id")
		if err != nil {
			return ChannelMessageRef{}, err
		}
		return ChannelMessageRef{MessageID: messageID}, nil
	}
	if mid, ok := payload["platform_message_id"].(string); ok && strings.TrimSpace(mid) != "" {
		return ChannelMessageRef{MessageID: strings.TrimSpace(mid)}, nil
	}
	return ChannelMessageRef{}, nil
}

func requiredUUIDValue(values map[string]any, key string) (uuid.UUID, error) {
	raw, err := requiredStringValue(values, key)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid uuid", key)
	}
	return id, nil
}

func requiredStringValue(values map[string]any, key string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return strings.TrimSpace(text), nil
}

func optionalStringValue(values map[string]any, key string) (string, bool) {
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func optionalBoolValue(values map[string]any, key string) (bool, bool) {
	raw, ok := values[key]
	if !ok {
		return false, false
	}
	value, ok := raw.(bool)
	if !ok {
		return false, false
	}
	return value, true
}

// NewChannelToolSurfaceFromContext 从 ChannelContext 构造工具可见片段（Desktop 与 Server 共用）。
func NewChannelToolSurfaceFromContext(c *ChannelContext) *tools.ChannelToolSurface {
	if c == nil {
		return nil
	}
	surface := &tools.ChannelToolSurface{
		ChannelID:        c.ChannelID,
		ChannelType:      c.ChannelType,
		PlatformChatID:   strings.TrimSpace(c.Conversation.Target),
		InboundMessageID: strings.TrimSpace(c.InboundMessage.MessageID),
	}
	if c.Conversation.ThreadID != nil {
		t := strings.TrimSpace(*c.Conversation.ThreadID)
		if t != "" {
			surface.MessageThreadID = &t
		}
	}
	return surface
}
