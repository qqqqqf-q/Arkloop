package pipeline

import (
	"context"
	"os"
	"strconv"
	"time"

	"arkloop/services/shared/telegrambot"
)

type TelegramChannelSender struct {
	client       *telegrambot.Client
	token        string
	segmentDelay time.Duration
}

func NewTelegramChannelSender(token string) *TelegramChannelSender {
	return NewTelegramChannelSenderWithClient(telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil), token, resolveSegmentDelay())
}

func NewTelegramChannelSenderWithClient(client *telegrambot.Client, token string, segmentDelay time.Duration) *TelegramChannelSender {
	if client == nil {
		client = telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	}
	return &TelegramChannelSender{
		client:       client,
		token:        token,
		segmentDelay: segmentDelay,
	}
}

func (s *TelegramChannelSender) SendText(ctx context.Context, target ChannelDeliveryTarget, text string) ([]string, error) {
	segments := splitTelegramMessage(telegrambot.FormatAssistantMarkdownAsHTML(text), 4096)
	ids := make([]string, 0, len(segments))
	for idx, segment := range segments {
		req := telegrambot.SendMessageRequest{
			ChatID:    target.Conversation.Target,
			Text:      segment,
			ParseMode: telegrambot.ParseModeHTML,
		}
		if idx == 0 && target.ReplyTo != nil {
			req.ReplyToMessageID = target.ReplyTo.MessageID
		}
		if target.Conversation.ThreadID != nil {
			req.MessageThreadID = *target.Conversation.ThreadID
		}
		sent, err := s.client.SendMessageWithHTMLFallback(ctx, s.token, req)
		if err != nil {
			return nil, err
		}
		if sent != nil && sent.MessageID != 0 {
			ids = append(ids, strconv.FormatInt(sent.MessageID, 10))
		}
		if idx < len(segments)-1 && s.segmentDelay > 0 {
			time.Sleep(s.segmentDelay)
		}
	}
	return ids, nil
}
