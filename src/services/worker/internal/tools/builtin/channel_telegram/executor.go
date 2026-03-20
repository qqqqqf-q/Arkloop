package channel_telegram

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

// LLM 输出的 tool args 常把 message_id 序列化成 JSON number（map 里是 float64），不能只断言 string。
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
		if math.IsNaN(x) || math.IsInf(x, 0) || x < 1 {
			return "", false
		}
		return formatFloatID(x), true
	case int:
		if x < 1 {
			return "", false
		}
		return strconv.Itoa(x), true
	case int64:
		if x < 1 {
			return "", false
		}
		return strconv.FormatInt(x, 10), true
	default:
		return "", false
	}
}

func formatFloatID(x float64) string {
	if x <= float64(math.MaxInt64) {
		return strconv.FormatInt(int64(x), 10)
	}
	return strconv.FormatFloat(x, 'f', 0, 64)
}

func firstNonEmptyArgString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		raw, ok := args[k]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return ""
}

// TokenLoader resolves the bot token for a channel (Server PG or Desktop SQLite).
type TokenLoader interface {
	BotToken(ctx context.Context, channelID uuid.UUID) (string, error)
}

// Executor handles telegram_react and telegram_reply.
type Executor struct {
	tokens TokenLoader
	tg     *telegrambot.Client
}

// NewExecutor builds an executor; tg nil uses default API base URL from env.
func NewExecutor(loader TokenLoader, tg *telegrambot.Client) *Executor {
	if tg == nil {
		tg = telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	}
	return &Executor{tokens: loader, tg: tg}
}

func (e *Executor) Execute(ctx context.Context, toolName string, args map[string]any, execCtx tools.ExecutionContext, _ string) tools.ExecutionResult {
	started := time.Now()
	ms := func() int { return int(time.Since(started).Milliseconds()) }

	if e == nil || e.tokens == nil || e.tg == nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "telegram channel tools not configured"},
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

	switch toolName {
	case ToolReact:
		return e.react(ctx, args, surface, chatID, token, started)
	case ToolReply:
		return e.reply(ctx, args, surface, chatID, token, started)
	default:
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolNotRegistered, Message: fmt.Sprintf("unknown tool %q", toolName)},
			DurationMs: ms(),
		}
	}
}

func (e *Executor) react(
	ctx context.Context,
	args map[string]any,
	surface *tools.ChannelToolSurface,
	chatID, token string,
	started time.Time,
) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }
	emoji := strings.TrimSpace(firstNonEmptyArgString(args, "emoji", "reaction"))
	if emoji == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "emoji or reaction is required"},
			DurationMs: ms(),
		}
	}
	midStr := ""
	if s, ok := coerceTelegramMessageID(args["message_id"]); ok {
		midStr = s
	}
	if midStr == "" {
		midStr = strings.TrimSpace(surface.InboundMessageID)
	}
	if midStr == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "message_id is required (no inbound message in context)"},
			DurationMs: ms(),
		}
	}
	mid, err := strconv.ParseInt(midStr, 10, 64)
	if err != nil || mid <= 0 {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "invalid message_id"},
			DurationMs: ms(),
		}
	}
	err = e.tg.SetMessageReaction(ctx, token, telegrambot.SetMessageReactionRequest{
		ChatID:    chatID,
		MessageID: mid,
		Reaction:  []telegrambot.MessageReactionEmoji{{Type: "emoji", Emoji: emoji}},
	})
	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
			DurationMs: ms(),
		}
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok": true, "message_id": midStr, "chat_id": chatID,
		},
		DurationMs: ms(),
	}
}

func (e *Executor) reply(
	ctx context.Context,
	args map[string]any,
	surface *tools.ChannelToolSurface,
	chatID, token string,
	started time.Time,
) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }
	text, _ := args["text"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "text is required"},
			DurationMs: ms(),
		}
	}
	replyToRaw := ""
	if s, ok := coerceTelegramMessageID(args["reply_to_message_id"]); ok {
		replyToRaw = s
	}
	if replyToRaw == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "reply_to_message_id is required"},
			DurationMs: ms(),
		}
	}
	if _, err := strconv.ParseInt(replyToRaw, 10, 64); err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "invalid reply_to_message_id"},
			DurationMs: ms(),
		}
	}

	formatted := telegrambot.FormatAssistantMarkdownAsHTML(text)
	segments := splitTelegramMessage(formatted, 4096)
	if len(segments) == 0 {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "empty text after processing"},
			DurationMs: ms(),
		}
	}

	var ids []string
	delay := replySegmentDelay()
	for i, segment := range segments {
		req := telegrambot.SendMessageRequest{
			ChatID:           chatID,
			Text:             segment,
			ParseMode:        telegrambot.ParseModeHTML,
			ReplyToMessageID: replyToRaw,
		}
		if surface.MessageThreadID != nil && strings.TrimSpace(*surface.MessageThreadID) != "" {
			req.MessageThreadID = strings.TrimSpace(*surface.MessageThreadID)
		}
		sent, err := e.tg.SendMessageWithHTMLFallback(ctx, token, req)
		if err != nil {
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
				DurationMs: ms(),
			}
		}
		if sent != nil && sent.MessageID != 0 {
			ids = append(ids, strconv.FormatInt(sent.MessageID, 10))
		}
		if i < len(segments)-1 && delay > 0 {
			time.Sleep(delay)
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "message_ids": ids, "segments": len(segments)},
		DurationMs: ms(),
	}
}

func replySegmentDelay() time.Duration {
	raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_MESSAGE_SEGMENT_DELAY_MS"))
	if raw == "" {
		return 50 * time.Millisecond
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 50 * time.Millisecond
	}
	return time.Duration(value) * time.Millisecond
}
