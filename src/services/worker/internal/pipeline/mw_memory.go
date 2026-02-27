package pipeline

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
)

const (
	memoryFindLimit       = 5
	memoryHighScoreL1     = 0.85  // 高分命中时额外拉 L1
	memoryCommitTimeout   = 15 * time.Second
)

// NewMemoryMiddleware 在 run 前注入长期记忆到 SystemPrompt，run 后异步归档对话。
// provider 为 nil 时整个 middleware 为 no-op。
func NewMemoryMiddleware(provider memory.MemoryProvider) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if provider == nil || rc.UserID == nil {
			return next(ctx, rc)
		}

		agentID := "default"
		if rc.SkillDefinition != nil && strings.TrimSpace(rc.SkillDefinition.ID) != "" {
			agentID = rc.SkillDefinition.ID
		}

		ident := memory.MemoryIdentity{
			OrgID:   rc.Run.OrgID,
			UserID:  *rc.UserID,
			AgentID: agentID,
		}

		userQuery := lastUserMessageText(rc.Messages)

		// 注入：有 query 才检索，避免空查询
		if userQuery != "" {
			injectMemory(ctx, rc, provider, ident, userQuery)
		}

		if err := next(ctx, rc); err != nil {
			return err
		}

		// 提取：user 和 assistant 消息均存在时才写入
		assistantOutput := strings.TrimSpace(rc.FinalAssistantOutput)
		if userQuery != "" && assistantOutput != "" {
			sessionID := rc.Run.ThreadID.String()
			msgs := []memory.MemoryMessage{
				{Role: "user", Content: userQuery},
				{Role: "assistant", Content: assistantOutput},
			}
			go commitMemoryAsync(ident, provider, sessionID, msgs)
		}

		return nil
	}
}

// injectMemory 调 Find，将检索结果组装成 memory block 追加到 rc.SystemPrompt 末尾。
func injectMemory(ctx context.Context, rc *RunContext, provider memory.MemoryProvider, ident memory.MemoryIdentity, query string) {
	hits, err := provider.Find(ctx, ident, memory.MemoryScopeUser, query, memoryFindLimit)
	if err != nil {
		slog.WarnContext(ctx, "memory: find failed", "err", err.Error())
		return
	}
	if len(hits) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("\n\n<memory>\n")
	for _, hit := range hits {
		if strings.TrimSpace(hit.Abstract) == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(hit.Abstract)

		// 高分非叶节点额外拉 L1
		if hit.Score >= memoryHighScoreL1 && !hit.IsLeaf {
			overview, ovErr := provider.Content(ctx, ident, hit.URI, memory.MemoryLayerOverview)
			if ovErr == nil && strings.TrimSpace(overview) != "" {
				sb.WriteString("\n  ")
				// 仅取 overview 首行，避免 token 暴涨
				firstLine := strings.SplitN(strings.TrimSpace(overview), "\n", 2)[0]
				sb.WriteString(firstLine)
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</memory>")

	block := sb.String()
	// 只有确实写了内容时才追加
	if strings.TrimSpace(block) == "\n\n<memory>\n</memory>" {
		return
	}
	rc.SystemPrompt += block
}

// commitMemoryAsync 在独立 goroutine 中归档对话，不阻塞 run 返回。
func commitMemoryAsync(ident memory.MemoryIdentity, provider memory.MemoryProvider, sessionID string, msgs []memory.MemoryMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), memoryCommitTimeout)
	defer cancel()

	if err := provider.AppendSessionMessages(ctx, ident, sessionID, msgs); err != nil {
		slog.Warn("memory: append session messages failed",
			"session_id", sessionID,
			"err", err.Error(),
		)
		return
	}
	if err := provider.CommitSession(ctx, ident, sessionID); err != nil {
		slog.Warn("memory: commit session failed",
			"session_id", sessionID,
			"err", err.Error(),
		)
	}
}

// lastUserMessageText 从消息列表中倒序找最后一条 user 消息的文本内容。
func lastUserMessageText(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "user" {
			continue
		}
		parts := make([]string, 0, len(msg.Content))
		for _, part := range msg.Content {
			if t := strings.TrimSpace(part.Text); t != "" {
				parts = append(parts, t)
			}
		}
		if text := strings.Join(parts, " "); text != "" {
			return text
		}
	}
	return ""
}
