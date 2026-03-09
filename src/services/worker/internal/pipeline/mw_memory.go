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
	memoryFindLimit    = 5
	memoryHighScoreL1  = 0.85 // 高分命中时额外拉 L1
	memoryFindTimeout  = 5 * time.Second
	memoryFlushTimeout = 120 * time.Second
	// snapshotFindTimeout 用于刷写后的最佳努力快照重建。
	snapshotFindTimeout = 15 * time.Second
)

var snapshotRepo = data.MemorySnapshotRepository{}
var usageRepo = data.UsageRecordsRepository{}

// NewMemoryMiddleware 在 run 前注入长期记忆到 SystemPrompt，run 后异步刷写显式 memory_write。
// provider 为 nil 时整个 middleware 为 no-op。
// pool 为 nil 时跳过快照缓存，每次直接 Find。
// configResolver 为 nil 时跳过 memory usage 记录。
func NewMemoryMiddleware(provider memory.MemoryProvider, pool *pgxpool.Pool, configResolver sharedconfig.Resolver) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		activeProvider := provider
		if activeProvider == nil {
			activeProvider = rc.MemoryProvider
		}
		if activeProvider == nil || rc.UserID == nil {
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
			injectFromCacheOrFind(ctx, rc, activeProvider, pool, ident, userQuery)
		}

		err := next(ctx, rc)
		flushPendingWritesAfterRun(activeProvider, pool, configResolver, rc)
		return err
	}
}

func flushPendingWritesAfterRun(provider memory.MemoryProvider, pool *pgxpool.Pool, configResolver sharedconfig.Resolver, rc *RunContext) {
	if rc.PendingMemoryWrites == nil {
		return
	}
	pending := rc.PendingMemoryWrites.Drain()
	if len(pending) == 0 {
		return
	}
	costPerWrite := resolveCommitCost(context.Background(), configResolver)
	go flushPendingWrites(pending, provider, pool, rc.Run.OrgID, rc.Run.ID, costPerWrite)
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
	block, hits := renderMemoryBlock(findCtx, provider, ident, memory.MemoryScopeUser, query)
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
func renderMemoryBlock(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity, scope memory.MemoryScope, query string) (string, []memory.MemoryHit) {
	lines, hits, err := findMemoryLines(ctx, provider, ident, scope, query)
	if err != nil {
		slog.WarnContext(ctx, "memory: find failed", "scope", string(scope), "err", err.Error())
		return "", nil
	}
	return buildMemoryBlock(lines), hits
}

func findMemoryLines(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity, scope memory.MemoryScope, query string) ([]string, []memory.MemoryHit, error) {
	hits, err := provider.Find(ctx, ident, scope, query, memoryFindLimit)
	if err != nil {
		return nil, nil, err
	}
	if len(hits) == 0 {
		return nil, nil, nil
	}

	lines := make([]string, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.Abstract) == "" {
			continue
		}

		line := strings.TrimSpace(hit.Abstract)
		if hit.Score >= memoryHighScoreL1 && !hit.IsLeaf {
			overview, ovErr := provider.Content(ctx, ident, hit.URI, memory.MemoryLayerOverview)
			if ovErr == nil && strings.TrimSpace(overview) != "" {
				firstLine := strings.SplitN(strings.TrimSpace(overview), "\n", 2)[0]
				line += "\n  " + firstLine
			}
		}
		lines = append(lines, line)
	}
	return lines, hits, nil
}

func buildMemoryBlock(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<memory>\n")
	for _, line := range lines {
		cleaned := strings.TrimSpace(line)
		if cleaned == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(cleaned)
		sb.WriteString("\n")
	}
	sb.WriteString("</memory>")
	block := sb.String()
	if strings.TrimSpace(block) == "<memory>\n</memory>" {
		return ""
	}
	return block
}

func flushPendingWrites(pending []memory.PendingWrite, provider memory.MemoryProvider, pool *pgxpool.Pool, orgID, runID uuid.UUID, costPerWrite float64) {
	ctx, cancel := context.WithTimeout(context.Background(), memoryFlushTimeout)
	defer cancel()

	successfulQueries := map[memory.MemoryScope][]string{}
	successCount := 0
	for _, pendingWrite := range pending {
		if err := provider.Write(ctx, pendingWrite.Ident, pendingWrite.Scope, pendingWrite.Entry); err != nil {
			slog.Warn("memory: deferred write failed",
				"org_id", pendingWrite.Ident.OrgID.String(),
				"user_id", pendingWrite.Ident.UserID.String(),
				"agent_id", pendingWrite.Ident.AgentID,
				"scope", string(pendingWrite.Scope),
				"err", err.Error(),
			)
			continue
		}
		successCount++
		query := strings.TrimSpace(pendingWrite.Entry.Content)
		if query != "" {
			successfulQueries[pendingWrite.Scope] = append(successfulQueries[pendingWrite.Scope], query)
		}
	}
	if successCount == 0 {
		return
	}

	if pool != nil {
		ident := pending[0].Ident
		if block, hits, ok := rebuildSnapshotBlock(ctx, provider, ident, successfulQueries); ok {
			if err := snapshotRepo.UpsertWithHits(ctx, pool, ident.OrgID, ident.UserID, ident.AgentID, block, hitsToCache(hits)); err != nil {
				slog.Warn("memory: snapshot rebuild upsert failed",
					"org_id", ident.OrgID.String(),
					"user_id", ident.UserID.String(),
					"agent_id", ident.AgentID,
					"err", err.Error(),
				)
			}
		}
	}

	if costPerWrite > 0 && pool != nil {
		totalCost := costPerWrite * float64(successCount)
		uCtx, uCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer uCancel()
		if err := usageRepo.InsertMemoryUsage(uCtx, pool, orgID, runID, totalCost); err != nil {
			slog.Warn("memory: usage record insert failed",
				"run_id", runID.String(),
				"err", err.Error(),
			)
		}
	}
}

func rebuildSnapshotBlock(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity, successfulQueries map[memory.MemoryScope][]string) (string, []memory.MemoryHit, bool) {
	if len(successfulQueries) == 0 {
		return "", nil, false
	}
	allLines := make([]string, 0, memoryFindLimit*len(successfulQueries))
	allHits := make([]memory.MemoryHit, 0, memoryFindLimit*len(successfulQueries))
	for scope, queries := range successfulQueries {
		query := strings.TrimSpace(strings.Join(queries, "\n"))
		if query == "" {
			return "", nil, false
		}
		snapCtx, cancel := context.WithTimeout(ctx, snapshotFindTimeout)
		lines, hits, err := findMemoryLines(snapCtx, provider, ident, scope, query)
		cancel()
		if err != nil {
			slog.Warn("memory: snapshot rebuild find failed",
				"org_id", ident.OrgID.String(),
				"user_id", ident.UserID.String(),
				"agent_id", ident.AgentID,
				"scope", string(scope),
				"err", err.Error(),
			)
			return "", nil, false
		}
		if len(lines) == 0 {
			return "", nil, false
		}
		allLines = append(allLines, lines...)
		allHits = append(allHits, hits...)
	}
	block := buildMemoryBlock(allLines)
	if block == "" {
		return "", nil, false
	}
	return block, allHits, true
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
			if t := strings.TrimSpace(llm.PartPromptText(part)); t != "" {
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
	value, parseErr := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if parseErr != nil || value <= 0 {
		return 0
	}
	return value
}
