package pipeline

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/shared/onebotclient"
)

const qqMessageMaxLen = 4500

// OneBotChannelSender 通过 OneBot11 HTTP API 发送 QQ 消息
type OneBotChannelSender struct {
	client       *onebotclient.Client
	segmentDelay time.Duration
}

func NewOneBotChannelSender(client *onebotclient.Client, segmentDelay time.Duration) *OneBotChannelSender {
	return &OneBotChannelSender{
		client:       client,
		segmentDelay: segmentDelay,
	}
}

func (s *OneBotChannelSender) SendText(ctx context.Context, target ChannelDeliveryTarget, text string) ([]string, error) {
	formatted := FormatOneBotAssistantText(text)
	segments := splitQQMessage(formatted, qqMessageMaxLen)
	ids := make([]string, 0, len(segments))

	msgType := "private"
	if target.Metadata != nil {
		if t, ok := target.Metadata["message_type"].(string); ok && t == "group" {
			msgType = "group"
		}
	}

	for idx, seg := range segments {
		msg := onebotclient.TextSegments(seg)

		// 群聊第一段消息插入 reply 引用
		if idx == 0 && msgType == "group" && target.ReplyTo != nil && strings.TrimSpace(target.ReplyTo.MessageID) != "" {
			msg = append([]onebotclient.MessageSegment{onebotclient.ReplySegment(target.ReplyTo.MessageID)}, msg...)
		}

		var resp *onebotclient.SendMsgResponse
		var err error

		switch msgType {
		case "group":
			resp, err = s.client.SendGroupMsg(ctx, target.Conversation.Target, msg)
		default:
			resp, err = s.client.SendPrivateMsg(ctx, target.Conversation.Target, msg)
		}
		if err != nil {
			return ids, err
		}
		if resp != nil {
			ids = append(ids, resp.MessageID.String())
		}
		if idx < len(segments)-1 && s.segmentDelay > 0 {
			time.Sleep(s.segmentDelay)
		}
	}
	return ids, nil
}

// splitQQMessage 按字符数拆分长消息
func splitQQMessage(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	runes := []rune(text)
	var parts []string
	for len(runes) > 0 {
		end := limit
		if end > len(runes) {
			end = len(runes)
		}
		// 尝试在换行处断开
		if end < len(runes) {
			for i := end - 1; i > end/2; i-- {
				if runes[i] == '\n' {
					end = i + 1
					break
				}
			}
		}
		parts = append(parts, string(runes[:end]))
		runes = runes[end:]
	}
	return parts
}

// SendMedia 通过 OneBot API 发送富媒体消息（图片/语音/视频）。
// kind: "image" / "record" / "video"；file 支持 URL 或本地路径。
func (s *OneBotChannelSender) SendMedia(ctx context.Context, target ChannelDeliveryTarget, kind, file, caption string) (string, error) {
	msgType := "private"
	if target.Metadata != nil {
		if t, ok := target.Metadata["message_type"].(string); ok && t == "group" {
			msgType = "group"
		}
	}

	var seg onebotclient.MessageSegment
	switch kind {
	case "image":
		seg = onebotclient.ImageSegment(file)
	case "record":
		seg = onebotclient.RecordSegment(file)
	case "video":
		seg = onebotclient.VideoSegment(file)
	default:
		seg = onebotclient.ImageSegment(file)
	}

	msg := []onebotclient.MessageSegment{seg}
	if caption != "" {
		msg = append(msg, onebotclient.TextSegments(caption)...)
	}

	var resp *onebotclient.SendMsgResponse
	var err error
	switch msgType {
	case "group":
		resp, err = s.client.SendGroupMsg(ctx, target.Conversation.Target, msg)
	default:
		resp, err = s.client.SendPrivateMsg(ctx, target.Conversation.Target, msg)
	}
	if err != nil {
		return "", err
	}
	if resp != nil {
		return resp.MessageID.String(), nil
	}
	return "", nil
}
