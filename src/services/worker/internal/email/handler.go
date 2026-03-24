package email

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/queue"
)

const maxEmailAttempts = 3

const (
	settingFrom    = "email.from"
	settingHost    = "email.smtp_host"
	settingPort    = "email.smtp_port"
	settingUser    = "email.smtp_user"
	settingPass    = "email.smtp_pass"
	settingTLSMode = "email.smtp_tls_mode"
)

// SmtpDefaultProvider 返回默认 SMTP 配置（从 smtp_providers 表）。
type SmtpDefaultProvider interface {
	DefaultSmtpConfig(ctx context.Context) (*Config, error)
}

// SendHandler 处理 email.send 类型的 job。
type SendHandler struct {
	resolver     sharedconfig.Resolver
	smtpProvider SmtpDefaultProvider // 可选，nil 时仅使用 resolver
	logger       *slog.Logger
}

func NewSendHandler(resolver sharedconfig.Resolver, logger *slog.Logger) (*SendHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &SendHandler{resolver: resolver, logger: logger}, nil
}

// SetSmtpProvider 注入 smtp_providers 表查询能力。
func (h *SendHandler) SetSmtpProvider(p SmtpDefaultProvider) {
	h.smtpProvider = p
}

func (h *SendHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	jobID := lease.JobID.String()

	msg, err := parseEmailPayload(lease.PayloadJSON)
	if err != nil {
		h.logger.Error("invalid email.send payload", "job_id", jobID, "error", err.Error())
		return nil // 格式错误不重试
	}

	cfg, cfgErr := h.resolveConfig(ctx)
	if cfgErr != nil {
		h.logger.Error("email config load failed", "job_id", jobID, "error", cfgErr.Error(), "attempts", lease.Attempts)
		if lease.Attempts+1 >= maxEmailAttempts {
			h.logger.Error("email max attempts reached, dropping", "job_id", jobID, "to", msg.To)
			return nil
		}
		return fmt.Errorf("email config: %w", cfgErr)
	}

	if !cfg.Enabled() {
		h.logger.Warn("email.send dropped: no SMTP configured", "job_id", jobID, "to", msg.To)
		return nil
	}

	mailer := NewMailer(cfg)
	if err := mailer.Send(ctx, msg); err != nil {
		h.logger.Error("email send failed", "job_id", jobID,
			"to", msg.To,
			"subject", msg.Subject,
			"attempts", lease.Attempts,
			"error", err.Error(),
		)
		if lease.Attempts+1 >= maxEmailAttempts {
			h.logger.Error("email max attempts reached, dropping", "job_id", jobID, "to", msg.To)
			return nil
		}
		return fmt.Errorf("email send: %w", err)
	}

	h.logger.Info("email sent", "job_id", jobID, "to", msg.To, "subject", msg.Subject)
	return nil
}

// resolveConfig 优先从 smtp_providers 表获取默认 provider，fallback 到 platform_settings。
func (h *SendHandler) resolveConfig(ctx context.Context) (Config, error) {
	if h.smtpProvider != nil {
		cfg, err := h.smtpProvider.DefaultSmtpConfig(ctx)
		if err == nil && cfg != nil && cfg.Enabled() {
			return *cfg, nil
		}
	}
	return loadEmailConfig(ctx, h.resolver)
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

func loadEmailConfig(ctx context.Context, resolver sharedconfig.Resolver) (Config, error) {
	if resolver == nil {
		return Config{}, nil
	}

	m, err := resolver.ResolvePrefix(ctx, "email.", sharedconfig.Scope{})
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		From:    strings.TrimSpace(m[settingFrom]),
		Host:    strings.TrimSpace(m[settingHost]),
		User:    strings.TrimSpace(m[settingUser]),
		Pass:    strings.TrimSpace(m[settingPass]),
		Port:    defaultPort,
		TLSMode: defaultTLSMode,
	}

	if p := strings.TrimSpace(m[settingPort]); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			cfg.Port = port
		}
	}
	if t := strings.TrimSpace(m[settingTLSMode]); t != "" {
		cfg.TLSMode = TLSMode(strings.ToLower(t))
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
