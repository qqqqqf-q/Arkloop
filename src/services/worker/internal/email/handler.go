package email

import (
	"context"
	"fmt"

	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/queue"
)

const maxEmailAttempts = 3

// SendHandler 处理 email.send 类型的 job。
type SendHandler struct {
	mailer Mailer
	logger *app.JSONLogger
}

func NewSendHandler(mailer Mailer, logger *app.JSONLogger) (*SendHandler, error) {
	if mailer == nil {
		return nil, fmt.Errorf("mailer must not be nil")
	}
	if logger == nil {
		logger = app.NewJSONLogger("email", nil)
	}
	return &SendHandler{mailer: mailer, logger: logger}, nil
}

func (h *SendHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	jobID := lease.JobID.String()
	fields := app.LogFields{JobID: &jobID}

	msg, err := parseEmailPayload(lease.PayloadJSON)
	if err != nil {
		h.logger.Error("invalid email.send payload", fields, map[string]any{"error": err.Error()})
		// 格式错误不重试
		return nil
	}

	if err := h.mailer.Send(ctx, msg); err != nil {
		h.logger.Error("email send failed", fields, map[string]any{
			"to":       msg.To,
			"subject":  msg.Subject,
			"attempts": lease.Attempts,
			"error":    err.Error(),
		})
		if lease.Attempts+1 >= maxEmailAttempts {
			h.logger.Error("email max attempts reached, dropping", fields, map[string]any{"to": msg.To})
			return nil
		}
		return fmt.Errorf("email send: %w", err)
	}

	h.logger.Info("email sent", fields, map[string]any{"to": msg.To, "subject": msg.Subject})
	return nil
}

func parseEmailPayload(raw map[string]any) (Message, error) {
	inner, ok := raw["payload"].(map[string]any)
	if !ok {
		return Message{}, fmt.Errorf("payload field missing or invalid")
	}

	to, _ := inner["to"].(string)
	if to == "" {
		return Message{}, fmt.Errorf("to is required")
	}
	subject, _ := inner["subject"].(string)
	if subject == "" {
		return Message{}, fmt.Errorf("subject is required")
	}
	html, _ := inner["html"].(string)
	text, _ := inner["text"].(string)
	if html == "" && text == "" {
		return Message{}, fmt.Errorf("html or text body is required")
	}

	return Message{To: to, Subject: subject, HTML: html, Text: text}, nil
}
