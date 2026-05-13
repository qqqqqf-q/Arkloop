package pipeline

import (
	"context"
	"strings"
	"time"

	"arkloop/services/shared/weixinclient"

	"github.com/google/uuid"
)

const weixinMessageMaxLen = 2048

// WeixinChannelSender 通过微信 iLink API 发送消息。
type WeixinChannelSender struct {
	client       *weixinclient.Client
	segmentDelay time.Duration
}

// NewWeixinChannelSender 使用环境变量创建微信渠道发送器。
func NewWeixinChannelSender(baseURL, token string) *WeixinChannelSender {
	return NewWeixinChannelSenderWithClient(weixinclient.NewClient(baseURL, token, nil), resolveSegmentDelay())
}

// NewWeixinChannelSenderWithClient 用于测试注入。
func NewWeixinChannelSenderWithClient(client *weixinclient.Client, segmentDelay time.Duration) *WeixinChannelSender {
	return &WeixinChannelSender{
		client:       client,
		segmentDelay: segmentDelay,
	}
}

// SendText 发送文本消息到微信用户。
// - 按 weixinMessageMaxLen 分片
// - 第一片带 ContextToken（从 target.Metadata["context_token"] 取）
// - 段间有 segmentDelay 延迟
func (s *WeixinChannelSender) SendText(ctx context.Context, target ChannelDeliveryTarget, text string) ([]string, error) {
	toUserID := target.Conversation.Target
	contextToken := ""
	if target.Metadata != nil {
		if ct, ok := target.Metadata["context_token"].(string); ok {
			contextToken = strings.TrimSpace(ct)
		}
	}

	segments := splitByRuneLimit(text, weixinMessageMaxLen)
	ids := make([]string, 0, len(segments))
	for idx, seg := range segments {
		clientID := "arkloop-weixin-" + uuid.NewString()
		req := weixinclient.SendMessageRequest{
			ToUserID:     toUserID,
			FromUserID:   "",
			MessageType:  2,
			MessageState: 2,
			ClientID:     clientID,
			ItemList: []weixinclient.MessageItem{
				{Type: 1, TextItem: &weixinclient.TextItem{Text: seg}},
			},
		}
		if idx == 0 && contextToken != "" {
			req.ContextToken = contextToken
		}

		resp, err := s.client.SendMessage(ctx, &req)
		if err != nil {
			return ids, err
		}
		messageID := clientID
		if resp != nil {
			if returnedID := strings.TrimSpace(resp.MessageID); returnedID != "" {
				messageID = returnedID
			}
		}
		ids = append(ids, messageID)
		if idx < len(segments)-1 && s.segmentDelay > 0 {
			time.Sleep(s.segmentDelay)
		}
	}
	return ids, nil
}
