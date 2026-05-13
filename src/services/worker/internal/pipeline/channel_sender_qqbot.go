package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"arkloop/services/shared/qqbotclient"
	"arkloop/services/worker/internal/data"
)

type QQBotChannelSender struct {
	client       *qqbotclient.Client
	segmentDelay time.Duration
}

func NewQQBotChannelSender(client *qqbotclient.Client, segmentDelay time.Duration) *QQBotChannelSender {
	return &QQBotChannelSender{client: client, segmentDelay: segmentDelay}
}

func NewQQBotChannelSenderFromChannel(channel *data.DeliveryChannelRecord, segmentDelay time.Duration) (*QQBotChannelSender, error) {
	if channel == nil {
		return nil, fmt.Errorf("qqbot channel is nil")
	}
	creds, err := qqbotclient.ParseCredentials(channel.ConfigJSON, channel.Token)
	if err != nil {
		return nil, err
	}
	opts := &qqbotclient.ClientOptions{
		TokenURL: strings.TrimSpace(os.Getenv("ARKLOOP_QQBOT_TOKEN_URL")),
		APIBase:  strings.TrimSpace(os.Getenv("ARKLOOP_QQBOT_API_BASE_URL")),
	}
	return NewQQBotChannelSender(qqbotclient.NewClient(creds, opts), segmentDelay), nil
}

func (s *QQBotChannelSender) SendText(ctx context.Context, target ChannelDeliveryTarget, text string) ([]string, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("qqbot sender is not configured")
	}
	segments := splitByRuneLimit(strings.TrimSpace(text), qqMessageMaxLen)
	ids := make([]string, 0, len(segments))
	scope := qqbotclient.ScopeC2C
	if qqbotTargetIsGroup(target) {
		scope = qqbotclient.ScopeGroup
	}
	for idx, segment := range segments {
		msgID := ""
		if target.ReplyTo != nil {
			msgID = strings.TrimSpace(target.ReplyTo.MessageID)
		}
		resp, err := s.client.SendText(ctx, scope, target.Conversation.Target, segment, msgID)
		if err != nil {
			return ids, err
		}
		if resp != nil && resp.PlatformMessageID() != "" {
			ids = append(ids, resp.PlatformMessageID())
		}
		if idx < len(segments)-1 && s.segmentDelay > 0 {
			time.Sleep(s.segmentDelay)
		}
	}
	return ids, nil
}

func qqbotTargetIsGroup(target ChannelDeliveryTarget) bool {
	if target.Metadata == nil {
		return false
	}
	for _, key := range []string{"scope", "message_type", "conversation_type"} {
		if value, ok := target.Metadata[key].(string); ok && strings.EqualFold(strings.TrimSpace(value), "group") {
			return true
		}
	}
	return false
}
