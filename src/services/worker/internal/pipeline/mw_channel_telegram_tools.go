package pipeline

import (
	"context"
	"strings"

	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/channel_telegram"
)

// NewChannelTelegramToolsMiddleware 在 Telegram Channel 的 run 上注入 telegram_react / telegram_reply / telegram_send_file；loader 为 nil 时跳过。
func NewChannelTelegramToolsMiddleware(loader channel_telegram.TokenLoader, telegram *telegrambot.Client) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if loader == nil || rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}
		if !strings.EqualFold(strings.TrimSpace(rc.ChannelContext.ChannelType), "telegram") {
			return next(ctx, rc)
		}

		deny := make(map[string]struct{})
		for _, n := range rc.ToolDenylist {
			c := strings.TrimSpace(n)
			if c != "" {
				deny[c] = struct{}{}
			}
		}

		exec := channel_telegram.NewExecutor(loader, telegram)
		var extraSpecs []tools.AgentToolSpec
		if _, blocked := deny[channel_telegram.ToolReact]; !blocked {
			rc.ToolExecutors[channel_telegram.ToolReact] = exec
			rc.AllowlistSet[channel_telegram.ToolReact] = struct{}{}
			rc.ToolSpecs = append(rc.ToolSpecs, channel_telegram.ReactLlmSpec)
			extraSpecs = append(extraSpecs, channel_telegram.ReactAgentSpec)
		}
		if _, blocked := deny[channel_telegram.ToolReply]; !blocked {
			rc.ToolExecutors[channel_telegram.ToolReply] = exec
			rc.AllowlistSet[channel_telegram.ToolReply] = struct{}{}
			rc.ToolSpecs = append(rc.ToolSpecs, channel_telegram.ReplyLlmSpec)
			extraSpecs = append(extraSpecs, channel_telegram.ReplyAgentSpec)
		}
		if _, blocked := deny[channel_telegram.ToolSendFile]; !blocked {
			rc.ToolExecutors[channel_telegram.ToolSendFile] = exec
			rc.AllowlistSet[channel_telegram.ToolSendFile] = struct{}{}
			rc.ToolSpecs = append(rc.ToolSpecs, channel_telegram.SendFileLlmSpec)
			extraSpecs = append(extraSpecs, channel_telegram.SendFileAgentSpec)
		}
		if len(extraSpecs) > 0 {
			rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, extraSpecs)
		}
		return next(ctx, rc)
	}
}
