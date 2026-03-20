package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"arkloop/services/worker/internal/llm"
)

const defaultChannelGroupMaxContextTokens = 4096

// NewChannelGroupContextTrimMiddleware 在 Channel 群聊 Run 上按近似 token 预算裁剪 Thread 历史（保留时间轴尾部）。
func NewChannelGroupContextTrimMiddleware() RunMiddleware {
	maxTokens := defaultChannelGroupMaxContextTokens
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxTokens = n
		}
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		_ = ctx
		if rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}
		if !isTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
			return next(ctx, rc)
		}
		trimRunContextMessagesToApproxTokens(rc, maxTokens)
		return next(ctx, rc)
	}
}

func isTelegramGroupLikeConversation(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}

func trimRunContextMessagesToApproxTokens(rc *RunContext, maxTokens int) {
	if rc == nil || maxTokens <= 0 || len(rc.Messages) == 0 {
		return
	}
	msgs := rc.Messages
	ids := rc.ThreadMessageIDs
	alignedIDs := len(ids) == len(msgs)

	total := 0
	start := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		t := approxLLMMessageTokens(msgs[i])
		if total+t > maxTokens {
			if start == len(msgs) {
				start = i
			}
			break
		}
		total += t
		start = i
	}
	if start <= 0 || start >= len(msgs) {
		return
	}
	rc.Messages = msgs[start:]
	if alignedIDs {
		rc.ThreadMessageIDs = ids[start:]
	}
}

func approxLLMMessageTokens(m llm.Message) int {
	n := 0
	for _, p := range m.Content {
		n += utf8.RuneCountInString(p.Text)
		n += utf8.RuneCountInString(p.ExtractedText)
		if p.Attachment != nil {
			n += 64
		}
		if len(p.Data) > 0 {
			n += len(p.Data) / 4
		}
	}
	for _, tc := range m.ToolCalls {
		n += utf8.RuneCountInString(tc.ToolName)
		if b, err := json.Marshal(tc.ArgumentsJSON); err == nil {
			n += len(b) / 4
		}
	}
	out := n / 3
	if out < 1 {
		return 1
	}
	return out
}
