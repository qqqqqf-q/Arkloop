package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"arkloop/services/shared/feishuclient"

	"github.com/google/uuid"
)

const feishuMessageMaxLen = 20000

type FeishuChannelSender struct {
	client       feishuTextClient
	appID        string
	appSecret    string
	segmentDelay time.Duration
}

type feishuTextClient interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text, uuid string) (*feishuclient.SentMessage, error)
	ReplyText(ctx context.Context, messageID, text string, replyInThread bool, uuid string) (*feishuclient.SentMessage, error)
}

type feishuChannelConfig struct {
	AppID  string `json:"app_id"`
	Domain string `json:"domain"`
}

type feishuChannelSecret struct {
	AppSecret string `json:"app_secret"`
}

func NewFeishuChannelSender(configJSON []byte, appSecret string) *FeishuChannelSender {
	return NewFeishuChannelSenderWithClient(nil, configJSON, appSecret, resolveSegmentDelay())
}

func NewFeishuChannelSenderWithClient(client feishuTextClient, configJSON []byte, appSecret string, segmentDelay time.Duration) *FeishuChannelSender {
	cfg := parseFeishuChannelConfig(configJSON)
	secret := parseFeishuAppSecret(appSecret)
	if client == nil {
		client = feishuclient.NewClient(feishuclient.Config{
			AppID:     cfg.AppID,
			AppSecret: secret,
			Domain:    cfg.Domain,
		}, &http.Client{Timeout: 10 * time.Second})
	}
	return &FeishuChannelSender{
		client:       client,
		appID:        cfg.AppID,
		appSecret:    secret,
		segmentDelay: segmentDelay,
	}
}

func (s *FeishuChannelSender) SendText(ctx context.Context, target ChannelDeliveryTarget, text string) ([]string, error) {
	if strings.TrimSpace(s.appID) == "" {
		return nil, fmt.Errorf("feishu sender: app_id must not be empty")
	}
	if strings.TrimSpace(s.appSecret) == "" {
		return nil, fmt.Errorf("feishu sender: app_secret must not be empty")
	}
	segments := splitByRuneLimit(text, feishuMessageMaxLen)
	if len(segments) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(segments))
	replyID := ""
	if target.ReplyTo != nil {
		replyID = strings.TrimSpace(target.ReplyTo.MessageID)
	}
	chatID := strings.TrimSpace(target.Conversation.Target)
	for idx, segment := range segments {
		var (
			sent *feishuclient.SentMessage
			err  error
		)
		switch {
		case idx == 0 && replyID != "":
			sent, err = s.client.ReplyText(ctx, replyID, segment, true, uuid.NewString())
		default:
			if chatID == "" {
				return ids, fmt.Errorf("feishu sender: chat_id must not be empty")
			}
			sent, err = s.client.SendText(ctx, "chat_id", chatID, segment, uuid.NewString())
		}
		if err != nil {
			return ids, err
		}
		if sent == nil || strings.TrimSpace(sent.MessageID) == "" {
			return ids, fmt.Errorf("feishu sender: message_id is empty")
		}
		ids = append(ids, strings.TrimSpace(sent.MessageID))
		if idx < len(segments)-1 && s.segmentDelay > 0 {
			time.Sleep(s.segmentDelay)
		}
	}
	return ids, nil
}

func parseFeishuChannelConfig(raw []byte) feishuChannelConfig {
	var cfg feishuChannelConfig
	if len(raw) == 0 {
		return cfg
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return feishuChannelConfig{}
	}
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.Domain = strings.TrimSpace(cfg.Domain)
	return cfg
}

func parseFeishuAppSecret(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var secret feishuChannelSecret
	if err := json.Unmarshal([]byte(raw), &secret); err != nil {
		return ""
	}
	return strings.TrimSpace(secret.AppSecret)
}

