package channel_qq

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/onebotclient"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

// OneBotConfigLoader resolves OneBot HTTP API base URL and token for a channel.
type OneBotConfigLoader interface {
	OneBotConfig(ctx context.Context, channelID uuid.UUID) (baseURL, token string, err error)
}

// Executor handles qq_react, qq_reply, qq_send_file.
type Executor struct {
	configs OneBotConfigLoader
}

func NewExecutor(loader OneBotConfigLoader) *Executor {
	return &Executor{configs: loader}
}

func (e *Executor) Execute(ctx context.Context, toolName string, args map[string]any, execCtx tools.ExecutionContext, _ string) tools.ExecutionResult {
	started := time.Now()
	ms := func() int { return int(time.Since(started).Milliseconds()) }

	if e == nil || e.configs == nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "qq channel tools not configured"},
			DurationMs: ms(),
		}
	}
	surface := execCtx.Channel
	if surface == nil || !strings.EqualFold(strings.TrimSpace(surface.ChannelType), "qq") {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "not a qq channel run"},
			DurationMs: ms(),
		}
	}
	chatID := strings.TrimSpace(surface.PlatformChatID)
	if chatID == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "missing qq chat in run context"},
			DurationMs: ms(),
		}
	}
	baseURL, token, err := e.configs.OneBotConfig(ctx, surface.ChannelID)
	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
			DurationMs: ms(),
		}
	}

	client := onebotclient.NewClient(baseURL, token, nil)

	switch toolName {
	case ToolReact:
		return e.react(ctx, args, surface, client, started)
	case ToolReply:
		return e.reply(args, started)
	case ToolSendFile:
		return e.sendFile(ctx, args, surface, chatID, client, started)
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
	client *onebotclient.Client,
	started time.Time,
) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }

	emojiID := argString(args, "emoji_id")
	if emojiID == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "emoji_id is required"},
			DurationMs: ms(),
		}
	}
	msgID := argString(args, "message_id")
	if msgID == "" {
		msgID = strings.TrimSpace(surface.InboundMessageID)
	}
	if msgID == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "message_id is required (no inbound message in context)"},
			DurationMs: ms(),
		}
	}
	if err := client.SetMsgEmojiLike(ctx, msgID, emojiID); err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
			DurationMs: ms(),
		}
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "message_id": msgID, "emoji_id": emojiID},
		DurationMs: ms(),
	}
}

func (e *Executor) reply(args map[string]any, started time.Time) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }
	replyTo := argString(args, "reply_to_message_id")
	if replyTo == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "reply_to_message_id is required"},
			DurationMs: ms(),
		}
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok":                  true,
			"reply_to_set":        true,
			"reply_to_message_id": replyTo,
		},
		DurationMs: ms(),
	}
}

func (e *Executor) sendFile(
	ctx context.Context,
	args map[string]any,
	surface *tools.ChannelToolSurface,
	chatID string,
	client *onebotclient.Client,
	started time.Time,
) tools.ExecutionResult {
	ms := func() int { return int(time.Since(started).Milliseconds()) }

	fileURL := argString(args, "file_url")
	if fileURL == "" {
		fileURL = argString(args, "url")
	}
	if fileURL == "" {
		fileURL = argString(args, "file")
	}
	if fileURL == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "file_url is required"},
			DurationMs: ms(),
		}
	}

	kind := strings.ToLower(argString(args, "kind"))
	if kind == "" {
		kind = strings.ToLower(argString(args, "type"))
	}
	if kind == "" {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: "kind is required (image, record, video)"},
			DurationMs: ms(),
		}
	}

	caption := argString(args, "caption")
	_ = caption

	var seg onebotclient.MessageSegment
	switch kind {
	case "image":
		seg = onebotclient.ImageSegment(fileURL)
	case "record":
		seg = onebotclient.RecordSegment(fileURL)
	case "video":
		seg = onebotclient.VideoSegment(fileURL)
	default:
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: fmt.Sprintf("unknown media kind: %q (expected: image, record, video)", kind)},
			DurationMs: ms(),
		}
	}

	msg := []onebotclient.MessageSegment{seg}
	if caption != "" {
		msg = append(msg, onebotclient.TextSegments(caption)...)
	}

	// 根据 surface 中是否有 group 信息选择发送方式
	// PlatformChatID 是 group_id（群消息）或 user_id（私聊）
	// 通过 InboundMessageID 判断 -> 实际上需要 metadata 中的 message_type
	// 简化方式：同时尝试 group，如果 chatID 格式不对会 fallback 到 private
	resp, err := client.SendGroupMsg(ctx, chatID, msg)
	if err != nil {
		resp, err = client.SendPrivateMsg(ctx, chatID, msg)
	}
	if err != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: tools.ErrorClassToolExecutionFailed, Message: err.Error()},
			DurationMs: ms(),
		}
	}

	var msgID string
	if resp != nil {
		msgID = resp.MessageID.String()
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "message_id": msgID, "chat_id": chatID, "kind": kind},
		DurationMs: ms(),
	}
}

func argString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		raw, ok := args[k]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case string:
			if t := strings.TrimSpace(v); t != "" {
				return t
			}
		case float64:
			if v > 0 {
				return fmt.Sprintf("%.0f", v)
			}
		}
	}
	return ""
}
