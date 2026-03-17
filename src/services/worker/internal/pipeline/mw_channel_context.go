package pipeline

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ChannelContext struct {
	ChannelID               uuid.UUID
	ChannelType             string
	PlatformChatID          string
	ReplyToMessageID        *string
	SenderChannelIdentityID uuid.UUID
	SenderUserID            *uuid.UUID
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
		channelCtx, err := parseChannelContext(rawDelivery)
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
		rc.ChannelContext = channelCtx
		if channelCtx.SenderUserID != nil {
			rc.UserID = channelCtx.SenderUserID
		}
		return next(ctx, rc)
	}
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
	platformChatID, err := requiredStringValue(payload, "platform_chat_id")
	if err != nil {
		return nil, err
	}
	senderIdentityID, err := requiredUUIDValue(payload, "sender_channel_identity_id")
	if err != nil {
		return nil, err
	}

	var replyToMessageID *string
	if raw, ok := payload["reply_to_message_id"].(string); ok && strings.TrimSpace(raw) != "" {
		value := strings.TrimSpace(raw)
		replyToMessageID = &value
	}

	return &ChannelContext{
		ChannelID:               channelID,
		ChannelType:             channelType,
		PlatformChatID:          platformChatID,
		ReplyToMessageID:        replyToMessageID,
		SenderChannelIdentityID: senderIdentityID,
	}, nil
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
