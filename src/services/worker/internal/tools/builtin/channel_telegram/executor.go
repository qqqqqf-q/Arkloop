package channel_telegram

import (
	"context"
	"fmt"
	"math"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
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

func normalizeArtifactRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "artifact:") {
		return ""
	}
	return strings.TrimPrefix(raw, "artifact:")
}

func tempFileExt(key, contentType string) string {
	if ext := strings.TrimSpace(filepath.Ext(key)); ext != "" {
		return ext
	}
	exts, err := mime.ExtensionsByType(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func tempArtifactFilename(key, contentType string) string {
	name := strings.TrimSpace(filepath.Base(key))
	if name != "" && name != "." && name != string(filepath.Separator) {
		return name
	}
	return "artifact" + tempFileExt(key, contentType)
}

// TokenLoader resolves the bot token for a channel (Server PG or Desktop SQLite).
type TokenLoader interface {
	BotToken(ctx context.Context, channelID uuid.UUID) (string, error)
}

// Executor handles telegram_react and telegram_reply.
type Executor struct {
	tokens TokenLoader
	tg     *telegrambot.Client
	store  objectstore.Store
}

// NewExecutor builds an executor; tg nil uses default API base URL from env.
func NewExecutor(loader TokenLoader, tg *telegrambot.Client, store objectstore.Store) *Executor {
	if tg == nil {
		tg = telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	}
	return &Executor{tokens: loader, tg: tg, store: store}
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
	case ToolSendFile:
		return e.sendFile(ctx, args, surface, chatID, token, started)
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

// reply sets the reply-to reference for the current run's delivery output.
// It does NOT send a message; the assistant's text output will be delivered
// by the channel delivery layer with this reply_to_message_id attached.
func (e *Executor) reply(
	_ context.Context,
	args map[string]any,
	_ *tools.ChannelToolSurface,
	_ string, _ string,
	started time.Time,
) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }
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
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok":                  true,
			"reply_to_set":        true,
			"reply_to_message_id": replyToRaw,
		},
		DurationMs: ms(),
	}
}

func (e *Executor) sendFile(
	ctx context.Context,
	args map[string]any,
	surface *tools.ChannelToolSurface,
	chatID, token string,
	started time.Time,
) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }

	fileURL := strings.TrimSpace(firstNonEmptyArgString(args, "file_url", "url", "file"))
	if fileURL == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "file_url is required"},
			DurationMs: ms(),
		}
	}
	cleanup := func() {}
	if artifactKey := normalizeArtifactRef(fileURL); artifactKey != "" {
		if e.store == nil {
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "artifact storage is not configured"},
				DurationMs: ms(),
			}
		}
		data, contentType, err := e.store.GetWithContentType(ctx, artifactKey)
		if err != nil {
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: fmt.Sprintf("artifact not found: %s", artifactKey)},
				DurationMs: ms(),
			}
		}
		tmpDir, err := os.MkdirTemp("", "arkloop-telegram-*")
		if err != nil {
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: fmt.Sprintf("create temp directory failed: %s", err.Error())},
				DurationMs: ms(),
			}
		}
		tmpPath := filepath.Join(tmpDir, tempArtifactFilename(artifactKey, contentType))
		tmp, err := os.Create(tmpPath)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: fmt.Sprintf("create temp file failed: %s", err.Error())},
				DurationMs: ms(),
			}
		}
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			_ = os.RemoveAll(tmpDir)
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: fmt.Sprintf("write temp file failed: %s", err.Error())},
				DurationMs: ms(),
			}
		}
		if err := tmp.Close(); err != nil {
			_ = os.RemoveAll(tmpDir)
			return tools.ExecutionResult{
				Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: fmt.Sprintf("close temp file failed: %s", err.Error())},
				DurationMs: ms(),
			}
		}
		fileURL = tmp.Name()
		cleanup = func() { _ = os.RemoveAll(tmpDir) }
	}
	defer cleanup()

	kind := strings.ToLower(strings.TrimSpace(firstNonEmptyArgString(args, "kind", "type")))
	if kind == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "kind is required (photo, document, audio, video, voice, animation)"},
			DurationMs: ms(),
		}
	}

	caption := strings.TrimSpace(firstNonEmptyArgString(args, "caption", "text"))

	// Process caption through Markdown -> HTML if it contains Markdown
	processedCaption := caption
	if caption != "" {
		processedCaption = telegrambot.FormatAssistantMarkdownAsHTML(caption)
	}

	var sent *telegrambot.SentMessage
	var err error

	// Get message thread ID for forum topics
	threadID := ""
	if surface.MessageThreadID != nil {
		threadID = strings.TrimSpace(*surface.MessageThreadID)
	}

	switch kind {
	case "photo":
		sent, err = e.tg.SendPhoto(ctx, token, chatID, fileURL, processedCaption, telegrambot.ParseModeHTML, threadID)
	case "document":
		sent, err = e.tg.SendDocument(ctx, token, chatID, fileURL, processedCaption, telegrambot.ParseModeHTML, threadID)
	case "audio":
		sent, err = e.tg.SendAudio(ctx, token, chatID, fileURL, processedCaption, telegrambot.ParseModeHTML, threadID)
	case "video":
		sent, err = e.tg.SendVideo(ctx, token, chatID, fileURL, processedCaption, telegrambot.ParseModeHTML, threadID)
	case "voice":
		sent, err = e.tg.SendVoice(ctx, token, chatID, fileURL, processedCaption, telegrambot.ParseModeHTML, threadID)
	case "animation":
		sent, err = e.tg.SendAnimation(ctx, token, chatID, fileURL, processedCaption, telegrambot.ParseModeHTML, threadID)
	default:
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: fmt.Sprintf("unknown media kind: %q (expected: photo, document, audio, video, voice, animation)", kind)},
			DurationMs: ms(),
		}
	}

	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
			DurationMs: ms(),
		}
	}

	var msgID int64
	if sent != nil {
		msgID = sent.MessageID
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok": true, "message_id": msgID, "chat_id": chatID, "kind": kind,
		},
		DurationMs: ms(),
	}
}
