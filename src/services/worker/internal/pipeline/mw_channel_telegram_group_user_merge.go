package pipeline

import (
	"context"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

// NewChannelTelegramGroupUserMergeMiddleware 将 Telegram 群聊线程尾部、自最后一条 assistant 起的连续多条 user
// 合并为单条 user 再交给后续中间件与 LLM。入库仍为每人一条；合并后 ThreadMessageIDs 仅保留尾段最后一条 user 的 id，
// 中间几条 id 不再出现在数组中（与 context compact 的 id 对齐语义一致）。
func NewChannelTelegramGroupUserMergeMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		_ = ctx
		if rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}
		if strings.ToLower(strings.TrimSpace(rc.ChannelContext.ChannelType)) != "telegram" {
			return next(ctx, rc)
		}
		if !IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
			return next(ctx, rc)
		}
		msgs, ids := mergeTelegramGroupTrailingUserBurst(rc.Messages, rc.ThreadMessageIDs)
		rc.Messages = msgs
		rc.ThreadMessageIDs = ids
		return next(ctx, rc)
	}
}

func mergeTelegramGroupTrailingUserBurst(msgs []llm.Message, ids []uuid.UUID) ([]llm.Message, []uuid.UUID) {
	if len(msgs) != len(ids) || len(msgs) < 2 {
		return msgs, ids
	}
	lastAsst := -1
	for i := range msgs {
		if strings.EqualFold(strings.TrimSpace(msgs[i].Role), "assistant") {
			lastAsst = i
		}
	}
	tailStart := lastAsst + 1
	tail := msgs[tailStart:]
	tailIDs := ids[tailStart:]
	if len(tail) < 2 {
		return msgs, ids
	}
	for _, m := range tail {
		if !strings.EqualFold(strings.TrimSpace(m.Role), "user") {
			return msgs, ids
		}
		if len(m.ToolCalls) > 0 {
			return msgs, ids
		}
	}
	mergedContent := mergeUserBurstContent(tail)
	merged := llm.Message{
		Role:    "user",
		Content: mergedContent,
	}
	outMsgs := make([]llm.Message, 0, len(msgs)-len(tail)+1)
	outMsgs = append(outMsgs, msgs[:tailStart]...)
	outMsgs = append(outMsgs, merged)
	outIDs := make([]uuid.UUID, 0, len(ids)-len(tail)+1)
	outIDs = append(outIDs, ids[:tailStart]...)
	outIDs = append(outIDs, tailIDs[len(tailIDs)-1])
	return outMsgs, outIDs
}

func mergeUserBurstContent(tail []llm.Message) []llm.ContentPart {
	const sep = "\n\n"
	var parts []llm.ContentPart
	for i := range tail {
		if i > 0 {
			parts = append(parts, llm.ContentPart{Type: messagecontent.PartTypeText, Text: sep})
		}
		for _, p := range tail[i].Content {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: ""}}
	}
	return parts
}
