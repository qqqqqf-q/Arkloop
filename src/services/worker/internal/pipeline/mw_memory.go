package pipeline

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"

	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// memorySnapshotFindLimit：后台刷新快照时 OpenViking Find 的 Top-K，越大快照越长（更多 L0 条目）。
	memorySnapshotFindLimit = 100
	memoryHighScoreL1       = 0.6  // 高分时拉详细内容（非叶子 L1 overview，叶子 L2 read）
	// memorySnapshotL1MaxRunes：单条命中附加的 L1 上限，避免单条记忆把 system 撑爆。
	memorySnapshotL1MaxRunes = 2000
	memoryFindTimeout        = 5 * time.Second
	memoryFlushTimeout       = 120 * time.Second
	// snapshotFindTimeout 用于刷写后的最佳努力快照重建。
	snapshotFindTimeout = 15 * time.Second
)

var usageRepo = data.UsageRecordsRepository{}

var snapshotRefreshWindow = 5 * time.Minute
var snapshotRefreshRetryInterval = 10 * time.Second
var snapshotRefreshMaxAttempts = 30

const (
	eventTypeMemoryDistillSkipped         = "memory.distill.skipped"
	eventTypeMemoryDistillStarted         = "memory.distill.started"
	eventTypeMemoryDistillAppendFailed    = "memory.distill.append_failed"
	eventTypeMemoryDistillCommitFailed    = "memory.distill.commit_failed"
	eventTypeMemoryDistillCommitted       = "memory.distill.committed"
	eventTypeMemoryDistillSnapshotUpdated = "memory.distill.snapshot_updated"
	eventTypeMemoryDistillSnapshotPending = "memory.distill.snapshot_pending"

	distillSkipReasonDisabled           = "disabled"
	distillSkipReasonNoAssistantOutput  = "no_assistant_output"
	distillSkipReasonNoIncrementalInput = "no_incremental_messages"
)

// NewMemoryMiddleware 在 run 前仅从快照注入 <memory>；run 后异步刷写显式 memory_write 并触发后台快照刷新。
// provider 为 nil 时整个 middleware 为 no-op。
// snap 为 nil 时不注入、不刷新快照表（与旧版 pool==nil 行为一致）。
// mdb 为 nil 时跳过 run_events / usage_records 写入，仍会执行 OpenViking 写与快照 Upsert。
// configResolver 为 nil 时跳过 memory usage 记录。
func NewMemoryMiddleware(provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, configResolver sharedconfig.Resolver) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		activeProvider := provider
		if activeProvider == nil {
			activeProvider = rc.MemoryProvider
		}
		if activeProvider == nil || rc.UserID == nil {
			return next(ctx, rc)
		}

		ident := memory.MemoryIdentity{
			AccountID: rc.Run.AccountID,
			UserID:    *rc.UserID,
			AgentID:   StableAgentID(rc),
		}

		userQuery := lastUserMessageText(rc.Messages)
		if userQuery != "" {
			injectFromSnapshotOnly(ctx, rc, snap, ident)
		}
		baseDistillUserMsgs := collectTrailingRealUserMessages(rc.Messages, rc.ThreadMessageIDs)

		err := next(ctx, rc)
		flushPendingWritesAfterRun(ctx, activeProvider, snap, mdb, configResolver, rc)
		distillAfterRun(activeProvider, snap, mdb, configResolver, rc, ident, baseDistillUserMsgs)
		return err
	}
}

func flushPendingWritesAfterRun(ctx context.Context, provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, configResolver sharedconfig.Resolver, rc *RunContext) {
	if rc.PendingMemoryWrites == nil {
		return
	}
	pending := rc.PendingMemoryWrites.Drain()
	if len(pending) == 0 {
		return
	}
	costPerWrite := resolveCommitCost(ctx, configResolver)
	go flushPendingWrites(pending, provider, snap, mdb, rc.Run.AccountID, rc.Run.ID, rc.TraceID, costPerWrite)
}

// injectFromSnapshotOnly 只追加已持久化的 memory_block，不在请求路径调用 OpenViking Find。
func injectFromSnapshotOnly(ctx context.Context, rc *RunContext, snap MemorySnapshotStore, ident memory.MemoryIdentity) {
	if snap == nil {
		return
	}
	block, found, err := snap.Get(ctx, ident.AccountID, ident.UserID, ident.AgentID)
	if err != nil {
		slog.WarnContext(ctx, "memory: snapshot read failed", "err", err.Error())
		return
	}
	if found && strings.TrimSpace(block) != "" {
		rc.SystemPrompt += block
	}
}

// renderMemoryBlock 通过 OpenViking Find 构建 <memory> 块，返回空串表示无结果。
func renderMemoryBlock(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity, targetURI string, query string) (string, []memory.MemoryHit) {
	lines, hits, err := findMemoryLines(ctx, provider, ident, targetURI, query)
	if err != nil {
		slog.WarnContext(ctx, "memory: find failed", "target_uri", targetURI, "err", err.Error())
		return "", nil
	}
	return buildMemoryBlock(lines), hits
}

func findMemoryLines(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity, targetURI string, query string) ([]string, []memory.MemoryHit, error) {
	hits, err := provider.Find(ctx, ident, targetURI, query, memorySnapshotFindLimit)
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
		if hit.Score >= memoryHighScoreL1 {
			var detail string
			var detailErr error
			if hit.IsLeaf {
				detail, detailErr = provider.Content(ctx, ident, hit.URI, memory.MemoryLayerRead)
			} else {
				detail, detailErr = provider.Content(ctx, ident, hit.URI, memory.MemoryLayerOverview)
			}
			if detailErr == nil {
				detail = clipRunes(strings.TrimSpace(detail), memorySnapshotL1MaxRunes)
				if detail != "" {
					line += "\n  " + indentContinuationLines(detail)
				}
			}
		}
		lines = append(lines, line)
	}
	return lines, hits, nil
}

func clipRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// indentContinuationLines：首行随 bullet，后续行加两级空格，避免多行 L1 在列表里顶格乱掉。
func indentContinuationLines(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "\n")
	var b strings.Builder
	b.WriteString(strings.TrimSpace(parts[0]))
	for i := 1; i < len(parts); i++ {
		if t := strings.TrimSpace(parts[i]); t != "" {
			b.WriteString("\n  ")
			b.WriteString(t)
		}
	}
	return b.String()
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

func flushPendingWrites(pending []memory.PendingWrite, provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, accountID, runID uuid.UUID, traceID string, costPerWrite float64) {
	// 由 goroutine 调用，超出请求生命周期，需要独立 context
	ctx, cancel := context.WithTimeout(context.Background(), memoryFlushTimeout)
	defer cancel()

	successfulQueries := map[string][]string{}
	successCount := 0
	emitter := events.NewEmitter(traceID)
	for _, pendingWrite := range pending {
		if err := provider.Write(ctx, pendingWrite.Ident, pendingWrite.Scope, pendingWrite.Entry); err != nil {
			slog.Warn("memory: deferred write failed",
				"account_id", pendingWrite.Ident.AccountID.String(),
				"user_id", pendingWrite.Ident.UserID.String(),
				"agent_id", pendingWrite.Ident.AgentID,
				"scope", string(pendingWrite.Scope),
				"err", err.Error(),
			)
			appendAsyncRunEvent(ctx, mdb, runID, emitter.Emit("memory.write.failed", map[string]any{
				"task_id":  strings.TrimSpace(pendingWrite.TaskID),
				"scope":    string(pendingWrite.Scope),
				"agent_id": pendingWrite.Ident.AgentID,
				"message":  err.Error(),
			}, stringPtr("memory_write"), stringPtr("tool.memory_provider_failure")))
			continue
		}
		successCount++
		appendAsyncRunEvent(ctx, mdb, runID, emitter.Emit("memory.write.completed", map[string]any{
			"task_id":  strings.TrimSpace(pendingWrite.TaskID),
			"scope":    string(pendingWrite.Scope),
			"agent_id": pendingWrite.Ident.AgentID,
		}, stringPtr("memory_write"), nil))
		query := strings.TrimSpace(pendingWrite.Entry.Content)
		if query != "" {
			successfulQueries[string(pendingWrite.Scope)] = append(successfulQueries[string(pendingWrite.Scope)], query)
		}
	}

	if snap != nil && len(pending) > 0 && successCount > 0 {
		ident := pending[0].Ident
		scheduleSnapshotRefresh(provider, snap, mdb, runID, traceID, ident, "", successfulQueries, "", "write")
	}

	if successCount == 0 {
		return
	}

	if costPerWrite > 0 && mdb != nil {
		totalCost := costPerWrite * float64(successCount)
		uCtx, uCancel := context.WithTimeout(ctx, 5*time.Second)
		defer uCancel()
		if err := usageRepo.InsertMemoryUsage(uCtx, mdb, accountID, runID, totalCost); err != nil {
			slog.Warn("memory: usage record insert failed",
				"run_id", runID.String(),
				"err", err.Error(),
			)
		}
	}
}

func rebuildSnapshotBlock(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity, successfulQueries map[string][]string) (string, []memory.MemoryHit, bool) {
	if len(successfulQueries) == 0 {
		return "", nil, false
	}
	allLines := make([]string, 0, memorySnapshotFindLimit*len(successfulQueries))
	allHits := make([]memory.MemoryHit, 0, memorySnapshotFindLimit*len(successfulQueries))
	for _, queries := range successfulQueries {
		query := strings.TrimSpace(strings.Join(queries, "\n"))
		if query == "" {
			return "", nil, false
		}
		snapCtx, cancel := context.WithTimeout(ctx, snapshotFindTimeout)
		lines, hits, err := findMemoryLines(snapCtx, provider, ident, memory.SelfURI(ident.UserID.String()), query)
		cancel()
		if err != nil {
			slog.Warn("memory: snapshot rebuild find failed",
				"account_id", ident.AccountID.String(),
				"user_id", ident.UserID.String(),
				"agent_id", ident.AgentID,
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

func tryRefreshSnapshotFromQueries(ctx context.Context, snap MemorySnapshotStore, provider memory.MemoryProvider, ident memory.MemoryIdentity, queries map[string][]string) (bool, error) {
	if snap == nil {
		return false, nil
	}
	block, hits, ok := rebuildSnapshotBlock(ctx, provider, ident, queries)
	if !ok {
		return false, nil
	}
	if err := snap.UpsertWithHits(ctx, ident.AccountID, ident.UserID, ident.AgentID, block, hitsToCache(hits)); err != nil {
		return false, err
	}
	return true, nil
}

func refreshSnapshotFromQueries(ctx context.Context, snap MemorySnapshotStore, provider memory.MemoryProvider, ident memory.MemoryIdentity, queries map[string][]string) {
	if _, err := tryRefreshSnapshotFromQueries(ctx, snap, provider, ident, queries); err != nil {
		slog.Warn("memory: snapshot rebuild upsert failed",
			"account_id", ident.AccountID.String(),
			"user_id", ident.UserID.String(),
			"agent_id", ident.AgentID,
			"err", err.Error(),
		)
	}
}

func appendAsyncRunEvent(ctx context.Context, mdb data.MemoryMiddlewareDB, runID uuid.UUID, ev events.RunEvent) {
	if mdb == nil || runID == uuid.Nil {
		return
	}
	tx, err := mdb.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.Warn("memory: begin run event tx failed", "run_id", runID.String(), "err", err.Error())
		return
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if _, err := (data.RunEventsRepository{}).AppendRunEvent(ctx, tx, runID, ev); err != nil {
		slog.Warn("memory: append run event failed", "run_id", runID.String(), "event_type", ev.Type, "err", err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("memory: commit run event tx failed", "run_id", runID.String(), "event_type", ev.Type, "err", err.Error())
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

// distillAfterRun 在 run 完成后判断是否触发 Memory 提炼。
// 条件：开启 distill、非 heartbeat、存在 FinalAssistantOutput、且有本轮增量 user 输入。
// 异步执行，不阻塞 run 返回。
func distillAfterRun(provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, configResolver sharedconfig.Resolver, rc *RunContext, ident memory.MemoryIdentity, baseUserMsgs []memory.MemoryMessage) {
	// heartbeat 是否写 memory 由 heartbeat_decision 决定，这里不再额外自动 distill。
	if rc.HeartbeatRun {
		return
	}
	emitter := events.NewEmitter(rc.TraceID)
	sessionID := rc.Run.ThreadID.String()
	if strings.TrimSpace(rc.FinalAssistantOutput) == "" {
		appendAsyncRunEvent(context.Background(), mdb, rc.Run.ID, emitter.Emit(eventTypeMemoryDistillSkipped, map[string]any{
			"kind":   "distill",
			"reason": distillSkipReasonNoAssistantOutput,
		}, nil, nil))
		return
	}

	enabled := resolveDistillEnabled(context.Background(), configResolver)
	if !enabled {
		appendAsyncRunEvent(context.Background(), mdb, rc.Run.ID, emitter.Emit(eventTypeMemoryDistillSkipped, map[string]any{
			"kind":   "distill",
			"reason": distillSkipReasonDisabled,
		}, nil, nil))
		return
	}

	msgs := buildDistillMessages(baseUserMsgs, rc.runtimeUserMessages, rc.FinalAssistantOutput)
	if len(msgs) == 0 {
		appendAsyncRunEvent(context.Background(), mdb, rc.Run.ID, emitter.Emit(eventTypeMemoryDistillSkipped, map[string]any{
			"kind":   "distill",
			"reason": distillSkipReasonNoIncrementalInput,
		}, nil, nil))
		return
	}

	costPerCommit := resolveCommitCost(context.Background(), configResolver)
	accountID := rc.Run.AccountID
	runID := rc.Run.ID
	appendAsyncRunEvent(context.Background(), mdb, runID, emitter.Emit(eventTypeMemoryDistillStarted, map[string]any{
		"kind":            "distill",
		"session_id":      sessionID,
		"message_count":   len(msgs) - 1,
		"tool_call_count": rc.RunToolCallCount,
		"iteration_count": rc.RunIterationCount,
	}, nil, nil))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), memoryFlushTimeout)
		defer cancel()

		if err := provider.AppendSessionMessages(ctx, ident, sessionID, msgs); err != nil {
			slog.Warn("memory: distill append failed",
				"account_id", accountID.String(),
				"session_id", sessionID,
				"err", err.Error(),
			)
			appendAsyncRunEvent(context.Background(), mdb, runID, emitter.Emit(eventTypeMemoryDistillAppendFailed, map[string]any{
				"kind":       "distill",
				"session_id": sessionID,
				"message":    err.Error(),
			}, nil, nil))
			return
		}

		if err := provider.CommitSession(ctx, ident, sessionID); err != nil {
			slog.Warn("memory: distill commit failed",
				"account_id", accountID.String(),
				"session_id", sessionID,
				"err", err.Error(),
			)
			appendAsyncRunEvent(context.Background(), mdb, runID, emitter.Emit(eventTypeMemoryDistillCommitFailed, map[string]any{
				"kind":       "distill",
				"session_id": sessionID,
				"message":    err.Error(),
			}, nil, nil))
			return
		}
		appendAsyncRunEvent(context.Background(), mdb, runID, emitter.Emit(eventTypeMemoryDistillCommitted, map[string]any{
			"kind":       "distill",
			"session_id": sessionID,
		}, nil, nil))

		scheduleSnapshotRefresh(provider, snap, mdb, runID, rc.TraceID, ident, sessionID, userMessagesToQueries(msgs), "memory.distill", "distill")

		if costPerCommit > 0 && mdb != nil {
			uCtx, uCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer uCancel()
			if err := usageRepo.InsertMemoryUsage(uCtx, mdb, accountID, runID, costPerCommit); err != nil {
				slog.Warn("memory: distill usage record failed",
					"run_id", runID.String(),
					"err", err.Error(),
				)
			}
		}
	}()
}

// resolveDistillEnabled 从配置中读取自动提炼开关。
func resolveDistillEnabled(ctx context.Context, resolver sharedconfig.Resolver) bool {
	if resolver == nil {
		return true
	}

	if raw, err := resolver.Resolve(ctx, "memory.distill_enabled", sharedconfig.Scope{}); err == nil {
		if strings.TrimSpace(strings.ToLower(raw)) == "false" {
			return false
		}
	}
	return true
}

// buildDistillMessages 只提取本 run 的增量人类输入和最终助手输出。
func buildDistillMessages(baseUserMsgs []memory.MemoryMessage, runtimeUserMsgs []memory.MemoryMessage, assistantOutput string) []memory.MemoryMessage {
	msgs := make([]memory.MemoryMessage, 0, len(baseUserMsgs)+len(runtimeUserMsgs)+1)
	msgs = append(msgs, baseUserMsgs...)
	msgs = append(msgs, runtimeUserMsgs...)

	if text := strings.TrimSpace(assistantOutput); text != "" && len(msgs) > 0 {
		msgs = append(msgs, memory.MemoryMessage{Role: "assistant", Content: text})
	}

	return msgs
}

func collectTrailingRealUserMessages(messages []llm.Message, ids []uuid.UUID) []memory.MemoryMessage {
	lastAssistantIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if i < len(ids) && ids[i] != uuid.Nil && messages[i].Role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}

	var out []memory.MemoryMessage
	for i := lastAssistantIdx + 1; i < len(messages); i++ {
		if i >= len(ids) || ids[i] == uuid.Nil || messages[i].Role != "user" {
			continue
		}
		var parts []string
		for _, part := range messages[i].Content {
			if text := strings.TrimSpace(llm.PartPromptText(part)); text != "" {
				parts = append(parts, text)
			}
		}
		if text := strings.Join(parts, "\n"); text != "" {
			out = append(out, memory.MemoryMessage{Role: "user", Content: text})
		}
	}
	return out
}

func userMessagesToQueries(msgs []memory.MemoryMessage) map[string][]string {
	queries := map[string][]string{}
	for _, msg := range msgs {
		if msg.Role != "user" {
			continue
		}
		if text := strings.TrimSpace(msg.Content); text != "" {
			queries[string(memory.MemoryScopeUser)] = append(queries[string(memory.MemoryScopeUser)], text)
		}
	}
	return queries
}

func scheduleSnapshotRefresh(
	provider memory.MemoryProvider,
	snap MemorySnapshotStore,
	mdb data.MemoryMiddlewareDB,
	runID uuid.UUID,
	traceID string,
	ident memory.MemoryIdentity,
	sessionID string,
	queries map[string][]string,
	eventPrefix string,
	kind string,
) {
	if provider == nil || snap == nil || runID == uuid.Nil || len(queries) == 0 {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), snapshotRefreshWindow)
		defer cancel()

		var lastErr error
		for attempt := 1; attempt <= snapshotRefreshMaxAttempts; attempt++ {
			updated, err := tryRefreshSnapshotFromQueries(ctx, snap, provider, ident, queries)
			if err != nil {
				lastErr = err
				slog.Warn("memory: snapshot refresh failed",
					"run_id", runID.String(),
					"kind", kind,
					"attempt", attempt,
					"err", err.Error(),
				)
			} else if updated {
				emitOptionalMemoryLifecycleEvent(mdb, runID, traceID, eventPrefix, ".snapshot_updated", map[string]any{
					"kind":       kind,
					"session_id": sessionID,
					"attempt":    attempt,
				})
				return
			}

			if attempt == snapshotRefreshMaxAttempts || ctx.Err() != nil {
				break
			}

			timer := time.NewTimer(snapshotRefreshRetryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				attempt = snapshotRefreshMaxAttempts
			case <-timer.C:
			}
		}

		pendingData := map[string]any{
			"kind":       kind,
			"session_id": sessionID,
		}
		if lastErr != nil {
			pendingData["message"] = lastErr.Error()
		}
		emitOptionalMemoryLifecycleEvent(mdb, runID, traceID, eventPrefix, ".snapshot_pending", pendingData)
	}()
}

func emitOptionalMemoryLifecycleEvent(mdb data.MemoryMiddlewareDB, runID uuid.UUID, traceID, eventPrefix, suffix string, data map[string]any) {
	if strings.TrimSpace(eventPrefix) == "" {
		return
	}
	appendAsyncRunEvent(context.Background(), mdb, runID, events.NewEmitter(traceID).Emit(eventPrefix+suffix, data, nil, nil))
}

// ForgetSnapshotRefresh 在 memory_forget 成功后调度后台快照重建；失败时保留原 memory_block。
func ForgetSnapshotRefresh(
	provider memory.MemoryProvider,
	store MemorySnapshotStore,
	mdb data.MemoryMiddlewareDB,
	runID uuid.UUID,
	traceID string,
	ident memory.MemoryIdentity,
) {
	queries := map[string][]string{
		string(memory.MemoryScopeUser): {"user profile preferences facts"},
	}
	scheduleSnapshotRefresh(provider, store, mdb, runID, traceID, ident, "", queries, "memory.forget", "forget")
}

// EditSnapshotRefresh schedules a background snapshot rebuild after memory_edit.
func EditSnapshotRefresh(
	provider memory.MemoryProvider,
	store MemorySnapshotStore,
	mdb data.MemoryMiddlewareDB,
	runID uuid.UUID,
	traceID string,
	ident memory.MemoryIdentity,
	query string,
) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	queries := map[string][]string{
		string(memory.MemoryScopeUser): {query},
	}
	scheduleSnapshotRefresh(provider, store, mdb, runID, traceID, ident, "", queries, "memory.edit", "edit")
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

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
