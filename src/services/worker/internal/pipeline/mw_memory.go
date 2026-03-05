package pipeline

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"

	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	memoryFindLimit     = 5
	memoryHighScoreL1   = 0.85 // 高分命中时额外拉 L1
	memoryCommitTimeout = 120 * time.Second
	memoryFindTimeout   = 5 * time.Second
	// snapshotFindTimeout 用于 commit 后快照更新，允许稍长
	snapshotFindTimeout = 15 * time.Second
)

var snapshotRepo = data.MemorySnapshotRepository{}
var usageRepo = data.UsageRecordsRepository{}

// NewMemoryMiddleware 在 run 前注入长期记忆到 SystemPrompt，run 后异步归档对话并快照到 PG。
// provider 为 nil 时整个 middleware 为 no-op。
// pool 为 nil 时跳过快照缓存，每次直接 Find。
// configResolver 为 nil 时跳过 memory usage 记录。
func NewMemoryMiddleware(provider memory.MemoryProvider, pool *pgxpool.Pool, configResolver sharedconfig.Resolver) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if provider == nil || rc.UserID == nil {
			return next(ctx, rc)
		}

		agentID := "default"
		if rc.PersonaDefinition != nil && strings.TrimSpace(rc.PersonaDefinition.ID) != "" {
			agentID = rc.PersonaDefinition.ID
		}

		ident := memory.MemoryIdentity{
			OrgID:   rc.Run.OrgID,
			UserID:  *rc.UserID,
			AgentID: agentID,
		}

		userQuery := lastUserMessageText(rc.Messages)

		if userQuery != "" {
			injectFromCacheOrFind(ctx, rc, provider, pool, ident, userQuery)
		}

		if err := next(ctx, rc); err != nil {
			return err
		}

		assistantOutput := strings.TrimSpace(rc.FinalAssistantOutput)
		if userQuery != "" && assistantOutput != "" {
			sessionID := rc.Run.ThreadID.String()
			msgs := []memory.MemoryMessage{
				{Role: "user", Content: userQuery},
				{Role: "assistant", Content: assistantOutput},
			}

			costPerCommit := resolveCommitCost(ctx, configResolver)
			go commitAndSnapshotAsync(ident, provider, pool, sessionID, msgs, userQuery, rc.Run.OrgID, rc.Run.ID, costPerCommit)
		}

		return nil
	}
}

// injectFromCacheOrFind 优先从 PG 快照读取记忆，缓存缺失时降级到 OpenViking Find。
func injectFromCacheOrFind(ctx context.Context, rc *RunContext, provider memory.MemoryProvider, pool *pgxpool.Pool, ident memory.MemoryIdentity, query string) {
	if pool != nil {
		block, found, err := snapshotRepo.Get(ctx, pool, ident.OrgID, ident.UserID, ident.AgentID)
		if err != nil {
			slog.WarnContext(ctx, "memory: snapshot read failed, falling back to find", "err", err.Error())
		} else if found && strings.TrimSpace(block) != "" {
			rc.SystemPrompt += block
			return
		}
	}

	findCtx, cancel := context.WithTimeout(ctx, memoryFindTimeout)
	defer cancel()
	block, hits := renderMemoryBlock(findCtx, provider, ident, query)
	if block != "" {
		rc.SystemPrompt += block
		if pool != nil {
			go func() {
				uCtx, uCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer uCancel()
				_ = snapshotRepo.UpsertWithHits(uCtx, pool, ident.OrgID, ident.UserID, ident.AgentID, block, hitsToCache(hits))
			}()
		}
	}
}

// renderMemoryBlock 通过 OpenViking Find 构建 <memory> 块，返回空串表示无结果。
// 同时返回 raw hits 供快照缓存使用。
func renderMemoryBlock(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity, query string) (string, []memory.MemoryHit) {
	hits, err := provider.Find(ctx, ident, memory.MemoryScopeUser, query, memoryFindLimit)
	if err != nil {
		slog.WarnContext(ctx, "memory: find failed", "err", err.Error())
		return "", nil
	}
	if len(hits) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("\n\n<memory>\n")
	for _, hit := range hits {
		if strings.TrimSpace(hit.Abstract) == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(hit.Abstract)

		if hit.Score >= memoryHighScoreL1 && !hit.IsLeaf {
			overview, ovErr := provider.Content(ctx, ident, hit.URI, memory.MemoryLayerOverview)
			if ovErr == nil && strings.TrimSpace(overview) != "" {
				sb.WriteString("\n  ")
				firstLine := strings.SplitN(strings.TrimSpace(overview), "\n", 2)[0]
				sb.WriteString(firstLine)
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</memory>")

	block := sb.String()
	if strings.TrimSpace(block) == "<memory>\n</memory>" {
		return "", nil
	}
	return block, hits
}

// commitAndSnapshotAsync 先快照当前记忆到 PG（在 commit 阻塞 OV 之前），再归档对话。
// commit 成功且 costPerCommit > 0 时写入 memory usage record。
func commitAndSnapshotAsync(ident memory.MemoryIdentity, provider memory.MemoryProvider, pool *pgxpool.Pool, sessionID string, msgs []memory.MemoryMessage, lastQuery string, orgID, runID uuid.UUID, costPerCommit float64) {
	ctx, cancel := context.WithTimeout(context.Background(), memoryCommitTimeout)
	defer cancel()

	// 先快照：commit 会阻塞 OpenViking 数分钟，必须在 commit 之前 Find
	if pool != nil {
		snapCtx, snapCancel := context.WithTimeout(ctx, snapshotFindTimeout)
		block, hits := renderMemoryBlock(snapCtx, provider, ident, lastQuery)
		snapCancel()
		if block != "" {
			if err := snapshotRepo.UpsertWithHits(ctx, pool, ident.OrgID, ident.UserID, ident.AgentID, block, hitsToCache(hits)); err != nil {
				slog.Warn("memory: snapshot upsert failed",
					"org_id", ident.OrgID.String(),
					"user_id", ident.UserID.String(),
					"err", err.Error(),
				)
			}
		}
	}

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
		return
	}

	if costPerCommit > 0 && pool != nil {
		uCtx, uCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer uCancel()
		if err := usageRepo.InsertMemoryUsage(uCtx, pool, orgID, runID, costPerCommit); err != nil {
			slog.Warn("memory: usage record insert failed",
				"run_id", runID.String(),
				"err", err.Error(),
			)
		}
	}
}

// hitsToCache 将 memory.MemoryHit 转换为 data.MemoryHitCache 用于 PG 存储。
func hitsToCache(hits []memory.MemoryHit) []data.MemoryHitCache {
	if len(hits) == 0 {
		return nil
	}
	cached := make([]data.MemoryHitCache, len(hits))
	for i, h := range hits {
		cached[i] = data.MemoryHitCache{
			URI:         h.URI,
			Abstract:    h.Abstract,
			Score:       h.Score,
			MatchReason: h.MatchReason,
			IsLeaf:      h.IsLeaf,
		}
	}
	return cached
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

// resolveCommitCost 从配置中获取每次 commit 的费用（USD），解析失败或未配置时返回 0。
func resolveCommitCost(ctx context.Context, resolver sharedconfig.Resolver) float64 {
	if resolver == nil {
		return 0
	}
	raw, err := resolver.Resolve(ctx, "openviking.cost_per_commit", sharedconfig.Scope{})
	if err != nil || strings.TrimSpace(raw) == "" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}
