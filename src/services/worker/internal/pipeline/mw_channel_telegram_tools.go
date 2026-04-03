package pipeline

import (
	"context"
	"strings"

	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/channel_telegram"
	conversationtool "arkloop/services/worker/internal/tools/conversation"
)

// ChannelTelegramToolsDeps 封装 Telegram 工具中间件所需的依赖。
type ChannelTelegramToolsDeps struct {
	TokenLoader        channel_telegram.TokenLoader
	TelegramClient     *telegrambot.Client
	GroupSearchExec    tools.Executor
	GroupSearchLlmSpec llm.ToolSpec
}

// NewChannelTelegramToolsMiddleware 在 Telegram Channel 的 run 上注入 telegram_react / telegram_reply / telegram_send_file。
// 群聊场景下额外注入 group_history_search 并移除 conversation_search（隐私隔离）。
func NewChannelTelegramToolsMiddleware(loader channel_telegram.TokenLoader, telegram *telegrambot.Client, opts ...ChannelTelegramToolsDeps) RunMiddleware {
	var dep ChannelTelegramToolsDeps
	if len(opts) > 0 {
		dep = opts[0]
	} else {
		dep.TokenLoader = loader
		dep.TelegramClient = telegram
	}

	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if dep.TokenLoader == nil || rc == nil || rc.ChannelContext == nil {
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

		exec := channel_telegram.NewExecutor(dep.TokenLoader, dep.TelegramClient)
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

		// 群聊场景：注入 group_history_search，移除 conversation_search
		if IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) && dep.GroupSearchExec != nil {
			const groupTool = "group_history_search"
			if _, blocked := deny[groupTool]; !blocked {
				rc.ToolExecutors[groupTool] = dep.GroupSearchExec
				rc.AllowlistSet[groupTool] = struct{}{}
				rc.ToolSpecs = append(rc.ToolSpecs, dep.GroupSearchLlmSpec)
				extraSpecs = append(extraSpecs, conversationtool.GroupSearchAgentSpec)
			}
			delete(rc.AllowlistSet, "conversation_search")
		}

		if len(extraSpecs) > 0 {
			rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, extraSpecs)
		}
		return next(ctx, rc)
	}
}
