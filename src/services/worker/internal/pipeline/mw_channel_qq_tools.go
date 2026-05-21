package pipeline

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/channel_qq"
	conversationtool "arkloop/services/worker/internal/tools/conversation"
)

// ChannelQQToolsDeps 封装 QQ 工具中间件所需的依赖。
type ChannelQQToolsDeps struct {
	ConfigLoader    channel_qq.OneBotConfigLoader
	GroupSearchExec tools.Executor
	GroupSearchSpec llm.ToolSpec
}

// NewChannelQQToolsMiddleware 在 QQ Channel 的 run 上注入 qq_react / qq_reply / qq_send_file。
// 群聊场景下额外注入 group_history_search 并移除跨线程 conversation_search。
func NewChannelQQToolsMiddleware(deps ChannelQQToolsDeps) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}
		channelType := strings.ToLower(strings.TrimSpace(rc.ChannelContext.ChannelType))
		if channelType != "qq" && channelType != "qqbot" {
			return next(ctx, rc)
		}

		isGroup := isQQGroupConversation(rc.ChannelContext.ConversationType)
		if isGroup {
			delete(rc.AllowlistSet, "conversation_search")
		}
		if deps.ConfigLoader == nil {
			return next(ctx, rc)
		}

		deny := make(map[string]struct{})
		for _, n := range rc.ToolDenylist {
			if c := strings.TrimSpace(n); c != "" {
				deny[c] = struct{}{}
			}
		}

		var extraSpecs []tools.AgentToolSpec

		if channelType == "qq" {
			exec := channel_qq.NewExecutor(deps.ConfigLoader)
			if _, blocked := deny[channel_qq.ToolReact]; !blocked {
				rc.ToolExecutors[channel_qq.ToolReact] = exec
				rc.AllowlistSet[channel_qq.ToolReact] = struct{}{}
				rc.ToolSpecs = append(rc.ToolSpecs, channel_qq.ReactLlmSpec)
				extraSpecs = append(extraSpecs, channel_qq.ReactAgentSpec)
			}
			if _, blocked := deny[channel_qq.ToolReply]; !blocked && !isGroup {
				rc.ToolExecutors[channel_qq.ToolReply] = exec
				rc.AllowlistSet[channel_qq.ToolReply] = struct{}{}
				rc.ToolSpecs = append(rc.ToolSpecs, channel_qq.ReplyLlmSpec)
				extraSpecs = append(extraSpecs, channel_qq.ReplyAgentSpec)
			}
			if _, blocked := deny[channel_qq.ToolSendFile]; !blocked {
				rc.ToolExecutors[channel_qq.ToolSendFile] = exec
				rc.AllowlistSet[channel_qq.ToolSendFile] = struct{}{}
				rc.ToolSpecs = append(rc.ToolSpecs, channel_qq.SendFileLlmSpec)
				extraSpecs = append(extraSpecs, channel_qq.SendFileAgentSpec)
			}
		}

		if isGroup {
			if deps.GroupSearchExec != nil {
				const groupTool = "group_history_search"
				if _, blocked := deny[groupTool]; !blocked {
					rc.ToolExecutors[groupTool] = deps.GroupSearchExec
					rc.AllowlistSet[groupTool] = struct{}{}
					rc.ToolSpecs = append(rc.ToolSpecs, deps.GroupSearchSpec)
					extraSpecs = append(extraSpecs, conversationtool.GroupSearchAgentSpec)
				}
			}
			delete(rc.AllowlistSet, "conversation_search")
		}

		if len(extraSpecs) > 0 {
			rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, extraSpecs)
		}
		return next(ctx, rc)
	}
}

func isQQGroupConversation(ct string) bool {
	return strings.EqualFold(strings.TrimSpace(ct), "group")
}
