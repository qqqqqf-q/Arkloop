//go:build desktop

package telegram_heartbeat_command

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TokenLoader resolves the bot token for a channel.
type TokenLoader interface {
	BotToken(ctx context.Context, channelID uuid.UUID) (string, error)
}

// DB wraps the database operations needed by the heartbeat command.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Executor handles telegram_heartbeat_command tool.
type Executor struct {
	db     DB
	tokens TokenLoader
	tg     *telegrambot.Client
}

// New builds an executor; tg nil uses default API base URL from env.
func New(db DB, loader TokenLoader, tg *telegrambot.Client) *Executor {
	if tg == nil {
		tg = telegrambot.NewClient("", nil)
	}
	return &Executor{db: db, tokens: loader, tg: tg}
}

func (e *Executor) Execute(ctx context.Context, toolName string, args map[string]any, execCtx tools.ExecutionContext, _ string) tools.ExecutionResult {
	started := time.Now()
	ms := func() int { return int(time.Since(started).Milliseconds()) }

	if e == nil || e.db == nil || e.tokens == nil || e.tg == nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "telegram heartbeat command not configured"},
			DurationMs: ms(),
		}
	}
	surface := execCtx.Channel
	if surface == nil || !strings.EqualFold(strings.TrimSpace(surface.ChannelType), "telegram") {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "not a telegram channel run"},
			DurationMs: ms(),
		}
	}

	// Get channel_identity_id from rc.ChannelContext via PipelineRC
	rc, ok := execCtx.PipelineRC.(*ChannelContextGetter)
	if !ok || rc == nil || rc.ChannelContext == nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "channel context not available"},
			DurationMs: ms(),
		}
	}
	identityID := rc.ChannelContext.SenderChannelIdentityID
	if identityID == uuid.Nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "sender channel identity not available"},
			DurationMs: ms(),
		}
	}

	action, _ := args["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))
	if action == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "action is required (status, on, off, interval, model)"},
			DurationMs: ms(),
		}
	}

	value, _ := args["value"].(string)
	value = strings.TrimSpace(value)

	chatID := strings.TrimSpace(surface.PlatformChatID)
	if chatID == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "missing telegram chat in run context"},
			DurationMs: ms(),
		}
	}

	token, err := e.tokens.BotToken(ctx, surface.ChannelID)
	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
			DurationMs: ms(),
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "empty bot token"},
			DurationMs: ms(),
		}
	}

	replyToRaw := ""
	if s, ok := coerceTelegramMessageID(args["reply_to_message_id"]); ok {
		replyToRaw = s
	}
	if replyToRaw == "" {
		replyToRaw = strings.TrimSpace(surface.InboundMessageID)
	}

	var replyText string
	switch action {
	case "status":
		replyText, err = e.getStatus(ctx, identityID)
	case "on":
		replyText, err = e.setEnabled(ctx, identityID, 1, 30)
	case "off":
		replyText, err = e.setEnabled(ctx, identityID, 0, 0)
	case "interval":
		interval, parseErr := strconv.Atoi(value)
		if parseErr != nil || interval <= 0 {
			replyText = "Invalid interval. Please provide a positive number (e.g., /heartbeat interval 30)"
			err = nil
		} else {
			replyText, err = e.setInterval(ctx, identityID, interval)
		}
	case "model":
		replyText, err = e.setModel(ctx, identityID, value)
	default:
		replyText = fmt.Sprintf("Unknown action: %s. Use: status, on, off, interval N, model NAME", action)
	}

	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
			DurationMs: ms(),
		}
	}

	// Send reply to Telegram
	if replyToRaw != "" {
		req := telegrambot.SendMessageRequest{
			ChatID:           chatID,
			Text:             replyText,
			ParseMode:        telegrambot.ParseModeHTML,
			ReplyToMessageID: replyToRaw,
		}
		if surface.MessageThreadID != nil && strings.TrimSpace(*surface.MessageThreadID) != "" {
			req.MessageThreadID = strings.TrimSpace(*surface.MessageThreadID)
		}
		_, sendErr := e.tg.SendMessage(ctx, token, req)
		if sendErr != nil {
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: sendErr.Error()},
				DurationMs: ms(),
			}
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok":     true,
			"action": action,
			"message": replyText,
		},
		DurationMs: ms(),
	}
}

func (e *Executor) getStatus(ctx context.Context, identityID uuid.UUID) (string, error) {
	row := e.db.QueryRow(ctx,
		`SELECT heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		 FROM channel_identities WHERE id = $1`,
		identityID.String(),
	)
	var enabled int
	var interval int
	var model string
	if err := row.Scan(&enabled, &interval, &model); err != nil {
		return "", fmt.Errorf("query heartbeat status: %w", err)
	}
	status := "disabled"
	if enabled == 1 {
		status = "enabled"
	}
	modelDisplay := "(follow conversation)"
	if model != "" {
		modelDisplay = model
	}
	return fmt.Sprintf("Heartbeat: %s\nInterval: %d min\nModel: %s", status, interval, modelDisplay), nil
}

func (e *Executor) setEnabled(ctx context.Context, identityID uuid.UUID, enabled, interval int) (string, error) {
	if interval == 0 {
		interval = runkind.DefaultHeartbeatIntervalMinutes
	}
	_, err := e.db.Exec(ctx,
		`UPDATE channel_identities
		 SET heartbeat_enabled = $1, heartbeat_interval_minutes = $2
		 WHERE id = $3`,
		enabled, interval, identityID.String(),
	)
	if err != nil {
		return "", fmt.Errorf("update heartbeat enabled: %w", err)
	}
	status := "disabled"
	if enabled == 1 {
		status = fmt.Sprintf("enabled (interval: %d min)", interval)
	}
	return fmt.Sprintf("Heartbeat %s", status), nil
}

func (e *Executor) setInterval(ctx context.Context, identityID uuid.UUID, interval int) (string, error) {
	_, err := e.db.Exec(ctx,
		`UPDATE channel_identities SET heartbeat_interval_minutes = $1, heartbeat_enabled = 1 WHERE id = $2`,
		interval, identityID.String(),
	)
	if err != nil {
		return "", fmt.Errorf("update heartbeat interval: %w", err)
	}
	return fmt.Sprintf("Heartbeat interval set to %d minutes", interval), nil
}

func (e *Executor) setModel(ctx context.Context, identityID uuid.UUID, model string) (string, error) {
	_, err := e.db.Exec(ctx,
		`UPDATE channel_identities SET heartbeat_model = $1, heartbeat_enabled = 1 WHERE id = $2`,
		model, identityID.String(),
	)
	if err != nil {
		return "", fmt.Errorf("update heartbeat model: %w", err)
	}
	modelDisplay := "(follow conversation)"
	if model != "" {
		modelDisplay = model
	}
	return fmt.Sprintf("Heartbeat model set to %s", modelDisplay), nil
}

// ChannelContextGetter is a subset of pipeline.RunContext needed to get ChannelContext.
type ChannelContextGetter struct {
	ChannelContext *ChannelContextSimple
}

// ChannelContextSimple is a minimal version of pipeline.ChannelContext.
type ChannelContextSimple struct {
	SenderChannelIdentityID uuid.UUID
}

// coerceTelegramMessageID handles JSON number serialization of message IDs.
func coerceTelegramMessageID(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return "", false
		}
		return s, true
	case float64:
		if x <= 0 || x != x || x > 1<<53 {
			return "", false
		}
		return strconv.FormatFloat(x, 'f', 0, 64), true
	default:
		return "", false
	}
}
