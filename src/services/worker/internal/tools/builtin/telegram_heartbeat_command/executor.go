//go:build desktop

package telegram_heartbeat_command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	shareddesktop "arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runkind"
	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/data"
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

	// 用群的 platform_chat_id 查群 identity（heartbeat 配置挂在群上）
	channelType := strings.TrimSpace(surface.ChannelType)
	if channelType == "" {
		channelType = "telegram"
	}
	slog.DebugContext(ctx, "heartbeat_command: looking up group identity", "channel_type", channelType, "chat_id", chatID)
	identityID, _, err := getGroupIdentityID(ctx, e.db, channelType, chatID)
	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "failed to get group identity: " + err.Error()},
			DurationMs: ms(),
		}
	}
	slog.DebugContext(ctx, "heartbeat_command: group identity resolved", "identity_id", identityID, "found", identityID != uuid.Nil)
	if identityID == uuid.Nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "group channel identity not found"},
			DurationMs: ms(),
		}
	}
	if execCtx.ThreadID == nil || *execCtx.ThreadID == uuid.Nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "thread context not available"},
			DurationMs: ms(),
		}
	}
	threadID := *execCtx.ThreadID

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
		replyText, err = e.getStatus(ctx, threadID)
	case "on":
		replyText, err = e.setEnabled(ctx, threadID, 1, 30)
	case "off":
		replyText, err = e.setEnabled(ctx, threadID, 0, 0)
	case "interval":
		interval, parseErr := strconv.Atoi(value)
		if parseErr != nil || interval <= 0 {
			replyText = "Invalid interval. Please provide a positive number (e.g., /heartbeat interval 30)"
			err = nil
		} else {
			replyText, err = e.setInterval(ctx, threadID, interval)
		}
	case "model":
		replyText, err = e.setModel(ctx, threadID, value)
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
			"ok":      true,
			"action":  action,
			"message": replyText,
		},
		DurationMs: ms(),
	}
}

func (e *Executor) getStatus(ctx context.Context, threadID uuid.UUID) (string, error) {
	cfg, err := e.getThreadHeartbeatConfig(ctx, threadID)
	if err != nil {
		return "", fmt.Errorf("query heartbeat status: %w", err)
	}
	status := "disabled"
	if cfg.HeartbeatEnabled != nil && *cfg.HeartbeatEnabled {
		status = "enabled"
	}
	interval := cfg.HeartbeatIntervalMinute
	if interval <= 0 {
		interval = runkind.DefaultHeartbeatIntervalMinutes
	}
	modelDisplay := "(follow conversation)"
	if cfg.HeartbeatModel != "" {
		modelDisplay = cfg.HeartbeatModel
	}
	return fmt.Sprintf("Heartbeat: %s\nInterval: %d min\nModel: %s", status, interval, modelDisplay), nil
}

func (e *Executor) setEnabled(ctx context.Context, threadID uuid.UUID, enabled, interval int) (string, error) {
	if interval == 0 {
		interval = runkind.DefaultHeartbeatIntervalMinutes
	}
	slog.DebugContext(ctx, "heartbeat_command: setEnabled", "thread_id", threadID, "enabled", enabled, "interval", interval)
	if err := e.updateThreadHeartbeatConfig(ctx, threadID, func(cfg *data.ThreadConfig) {
		value := enabled == 1
		cfg.HeartbeatEnabled = &value
		cfg.HeartbeatIntervalMinute = interval
	}); err != nil {
		return "", fmt.Errorf("update heartbeat enabled: %w", err)
	}
	if enabled == 0 {
		if _, err := e.db.Exec(ctx,
			`DELETE FROM scheduled_triggers WHERE thread_id = $1`,
			threadID.String(),
		); err != nil {
			return "", fmt.Errorf("delete heartbeat trigger: %w", err)
		}
		e.notifyHeartbeatScheduler(ctx)
	}
	status := "disabled"
	if enabled == 1 {
		status = fmt.Sprintf("enabled (interval: %d min)", interval)
	}
	return fmt.Sprintf("Heartbeat %s", status), nil
}

func (e *Executor) setInterval(ctx context.Context, threadID uuid.UUID, interval int) (string, error) {
	if err := e.updateThreadHeartbeatConfig(ctx, threadID, func(cfg *data.ThreadConfig) {
		enabled := true
		cfg.HeartbeatEnabled = &enabled
		cfg.HeartbeatIntervalMinute = interval
	}); err != nil {
		return "", fmt.Errorf("update heartbeat interval: %w", err)
	}
	if _, err := e.db.Exec(ctx,
		`UPDATE scheduled_triggers
		    SET interval_min = $1,
		        next_fire_at = $2,
		        updated_at = $3
		  WHERE thread_id = $4`,
		interval,
		time.Now().UTC().Add(time.Duration(interval)*time.Minute).Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		threadID.String(),
	); err != nil {
		return "", fmt.Errorf("update heartbeat trigger interval: %w", err)
	}
	e.notifyHeartbeatScheduler(ctx)
	return fmt.Sprintf("Heartbeat interval set to %d minutes", interval), nil
}

func (e *Executor) setModel(ctx context.Context, threadID uuid.UUID, model string) (string, error) {
	model = strings.TrimSpace(model)
	if err := e.updateThreadHeartbeatConfig(ctx, threadID, func(cfg *data.ThreadConfig) {
		enabled := true
		cfg.HeartbeatEnabled = &enabled
		cfg.HeartbeatModel = model
	}); err != nil {
		return "", fmt.Errorf("update heartbeat model: %w", err)
	}
	if _, err := e.db.Exec(ctx,
		`UPDATE scheduled_triggers
			    SET model = $1,
			        updated_at = $2
			  WHERE thread_id = $3`,
		model,
		time.Now().UTC().Format(time.RFC3339Nano),
		threadID.String(),
	); err != nil {
		return "", fmt.Errorf("update heartbeat trigger model: %w", err)
	}
	e.notifyHeartbeatScheduler(ctx)
	modelDisplay := "(follow conversation)"
	if model != "" {
		modelDisplay = model
	}
	return fmt.Sprintf("Heartbeat model set to %s", modelDisplay), nil
}

func (e *Executor) getThreadHeartbeatConfig(ctx context.Context, threadID uuid.UUID) (data.ThreadConfig, error) {
	if threadID == uuid.Nil {
		return data.ThreadConfig{}, fmt.Errorf("thread_id is empty")
	}
	var raw []byte
	if err := e.db.QueryRow(ctx, `SELECT COALESCE(config_json, '{}') FROM threads WHERE id = $1`, threadID.String()).Scan(&raw); err != nil {
		return data.ThreadConfig{}, err
	}
	var cfg data.ThreadConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return data.ThreadConfig{}, err
	}
	cfg.HeartbeatModel = strings.TrimSpace(cfg.HeartbeatModel)
	return cfg, nil
}

func (e *Executor) updateThreadHeartbeatConfig(ctx context.Context, threadID uuid.UUID, mutate func(*data.ThreadConfig)) error {
	if threadID == uuid.Nil {
		return fmt.Errorf("thread_id is empty")
	}
	var raw []byte
	if err := e.db.QueryRow(ctx, `SELECT COALESCE(config_json, '{}') FROM threads WHERE id = $1`, threadID.String()).Scan(&raw); err != nil {
		return err
	}
	config := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return err
		}
	}
	cfg := data.ThreadConfig{}
	if enabled, ok := config["heartbeat_enabled"].(bool); ok {
		cfg.HeartbeatEnabled = &enabled
	}
	if interval, ok := config["heartbeat_interval_minutes"].(float64); ok {
		cfg.HeartbeatIntervalMinute = int(interval)
	}
	if model, ok := config["heartbeat_model"].(string); ok {
		cfg.HeartbeatModel = strings.TrimSpace(model)
	}
	if mutate != nil {
		mutate(&cfg)
	}
	if cfg.HeartbeatEnabled == nil {
		delete(config, "heartbeat_enabled")
	} else {
		config["heartbeat_enabled"] = *cfg.HeartbeatEnabled
	}
	if cfg.HeartbeatIntervalMinute > 0 {
		config["heartbeat_interval_minutes"] = cfg.HeartbeatIntervalMinute
	} else {
		delete(config, "heartbeat_interval_minutes")
	}
	if strings.TrimSpace(cfg.HeartbeatModel) == "" {
		delete(config, "heartbeat_model")
	} else {
		config["heartbeat_model"] = strings.TrimSpace(cfg.HeartbeatModel)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = e.db.Exec(ctx, `UPDATE threads SET config_json = $2, updated_at = $3 WHERE id = $1`, threadID.String(), string(encoded), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (e *Executor) notifyHeartbeatScheduler(ctx context.Context) {
	bus, ok := shareddesktop.GetEventBus().(eventbus.EventBus)
	if !ok || bus == nil {
		return
	}
	_ = bus.Publish(ctx, pgnotify.ChannelHeartbeat, "")
}

// getGroupIdentityID 通过 channel_type + platform_subject_id 查群的 channel_identities.id。
func getGroupIdentityID(ctx context.Context, db DB, channelType, platformChatID string) (uuid.UUID, bool, error) {
	var idStr string
	err := db.QueryRow(ctx,
		`SELECT id FROM channel_identities WHERE channel_type = $1 AND platform_subject_id = $2`,
		channelType, platformChatID,
	).Scan(&idStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("get group identity: %w", err)
	}
	id, parseErr := uuid.Parse(idStr)
	if parseErr != nil {
		return uuid.Nil, false, fmt.Errorf("parse group identity id: %w", parseErr)
	}
	return id, true, nil
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
