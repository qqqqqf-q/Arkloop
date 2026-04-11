package pipeline

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"

	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	memoryFlushTimeout = 120 * time.Second

	// 目录树快照常量
	memorySkeletonTimeout = 10 * time.Second // 骨架读取总超时
	memorySkeletonMaxDirs = 10               // 最多读取的一级子目录数
	memoryLeafMaxPerDir   = 30               // 每个目录下最多读取的叶子 abstract 数
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
// impStore 为 nil 时不注入 impression、不累积 score。
func NewMemoryMiddleware(provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, configResolver sharedconfig.Resolver, impStore ImpressionStore, impRefresh ImpressionRefreshFunc) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc.ImpressionRun || isImpressionRun(rc) {
			return next(ctx, rc)
		}

		activeProvider := provider
		if activeProvider == nil {
			activeProvider = rc.MemoryProvider
		}
		if activeProvider == nil || rc.UserID == nil {
			return next(ctx, rc)
		}

		err := next(ctx, rc)
		flushPendingWritesAfterRun(ctx, activeProvider, snap, mdb, configResolver, rc, impStore, impRefresh)
		return err
	}
}

func flushPendingWritesAfterRun(ctx context.Context, provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, configResolver sharedconfig.Resolver, rc *RunContext, impStore ImpressionStore, impRefresh ImpressionRefreshFunc) {
	if rc.PendingMemoryWrites == nil {
		return
	}
	pending := rc.PendingMemoryWrites.Drain()
	if len(pending) == 0 {
		return
	}
	costPerWrite := resolveCommitCost(ctx, configResolver)

	ident := memory.MemoryIdentity{
		AccountID: rc.Run.AccountID,
		UserID:    *rc.UserID,
		AgentID:   StableAgentID(rc),
	}
	go flushPendingWrites(pending, provider, snap, mdb, rc.Run.AccountID, rc.Run.ID, rc.TraceID, costPerWrite, impStore, ident, configResolver, impRefresh)
}

func flushPendingWrites(pending []memory.PendingWrite, provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, accountID, runID uuid.UUID, traceID string, costPerWrite float64, impStore ImpressionStore, ident memory.MemoryIdentity, configResolver sharedconfig.Resolver, impRefresh ImpressionRefreshFunc) {
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

	if impStore != nil && successCount > 0 {
		addImpressionScore(ctx, impStore, ident, 5*successCount, configResolver, impRefresh)
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

	skelCtx, skelCancel := context.WithTimeout(ctx, memorySkeletonTimeout)
	skeletonLines, leafLines, hits, ok := buildSnapshotFromTree(skelCtx, provider, ident)
	skelCancel()

	if !ok || (len(skeletonLines) == 0 && len(leafLines) == 0) {
		return "", nil, false
	}

	block := buildTreeShapedMemoryBlock(skeletonLines, leafLines)
	if block == "" {
		return "", nil, false
	}
	return block, hits, true
}

// buildSnapshotFromTree 通过 ls + Content 构建完整的目录树快照。
// 目录项读 overview，叶子文件读 read（L2 完整内容），对一级子目录递归一层收集叶子。
// 同时收集 hits 用于前端 UI 展示。
func buildSnapshotFromTree(ctx context.Context, provider memory.MemoryProvider, ident memory.MemoryIdentity) (skeletonLines []string, leafLines []string, hits []memory.MemoryHit, ok bool) {
	rootURI := memory.SelfURI(ident.UserID.String())

	rootOverview, err := provider.Content(ctx, ident, rootURI, memory.MemoryLayerOverview)
	if err != nil || strings.TrimSpace(rootOverview) == "" {
		return nil, nil, nil, false
	}

	skeletonLines = []string{strings.TrimSpace(rootOverview)}

	children, err := provider.ListDir(ctx, ident, rootURI)
	if err != nil {
		return skeletonLines, nil, hits, true
	}

	dirCount := 0
	for _, childURI := range children {
		if strings.HasSuffix(childURI, "/") {
			if dirCount >= memorySkeletonMaxDirs {
				continue
			}
			dirCount++
			childOverview, childErr := provider.Content(ctx, ident, childURI, memory.MemoryLayerOverview)
			if childErr == nil && strings.TrimSpace(childOverview) != "" {
				skeletonLines = append(skeletonLines, strings.TrimSpace(childOverview))
				hits = append(hits, memory.MemoryHit{
					URI:      strings.TrimSuffix(childURI, "/"),
					Abstract: memoryFirstLine(strings.TrimSpace(childOverview)),
					IsLeaf:   false,
				})
			}
			subChildren, subErr := provider.ListDir(ctx, ident, childURI)
			if subErr != nil {
				continue
			}
			leafCount := 0
			for _, subURI := range subChildren {
				if leafCount >= memoryLeafMaxPerDir {
					break
				}
				if strings.HasSuffix(subURI, "/") {
					continue
				}
				content, readErr := provider.Content(ctx, ident, subURI, memory.MemoryLayerRead)
				if readErr == nil && strings.TrimSpace(content) != "" {
					leafLines = append(leafLines, strings.TrimSpace(content))
					leafCount++
					hits = append(hits, memory.MemoryHit{
						URI:      subURI,
						Abstract: memoryFirstLine(strings.TrimSpace(content)),
						IsLeaf:   true,
					})
				}
			}
		} else {
			content, readErr := provider.Content(ctx, ident, childURI, memory.MemoryLayerRead)
			if readErr == nil && strings.TrimSpace(content) != "" {
				leafLines = append(leafLines, strings.TrimSpace(content))
				hits = append(hits, memory.MemoryHit{
					URI:      childURI,
					Abstract: memoryFirstLine(strings.TrimSpace(content)),
					IsLeaf:   true,
				})
			}
		}
	}
	return skeletonLines, leafLines, hits, true
}

func memoryFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	runes := []rune(s)
	if len(runes) > 100 {
		return string(runes[:100])
	}
	return s
}

// buildTreeShapedMemoryBlock 拼装骨架 + 叶子补充的 <memory> block。
func buildTreeShapedMemoryBlock(skeletonLines []string, leafLines []string) string {
	if len(skeletonLines) == 0 && len(leafLines) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<memory>\n")
	for _, line := range skeletonLines {
		cleaned := strings.TrimSpace(line)
		if cleaned == "" {
			continue
		}
		sb.WriteString(cleaned)
		sb.WriteString("\n\n")
	}
	if len(leafLines) > 0 {
		if len(skeletonLines) > 0 {
			sb.WriteString("---\n")
		}
		for _, line := range leafLines {
			cleaned := strings.TrimSpace(line)
			if cleaned == "" {
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(cleaned)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("</memory>")
	block := sb.String()
	if strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(block, "<memory>", ""), "</memory>", "")) == "" {
		return ""
	}
	return block
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
func distillAfterRun(provider memory.MemoryProvider, snap MemorySnapshotStore, mdb data.MemoryMiddlewareDB, configResolver sharedconfig.Resolver, rc *RunContext, ident memory.MemoryIdentity, baseUserMsgs []memory.MemoryMessage, impStore ImpressionStore, impRefresh ImpressionRefreshFunc) {
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

		if impStore != nil {
			weight := impressionScoreForRun(rc)
			addImpressionScore(ctx, impStore, ident, weight, configResolver, impRefresh)
		}

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

// --- impression score ---

func impressionScoreForRun(rc *RunContext) int {
	if rc.ChannelContext != nil && rc.ChannelContext.ConversationType == "supergroup" {
		return 1
	}
	return 3
}

func addImpressionScore(ctx context.Context, store ImpressionStore, ident memory.MemoryIdentity, delta int, resolver sharedconfig.Resolver, refresh ImpressionRefreshFunc) {
	newScore, err := store.AddScore(ctx, ident.AccountID, ident.UserID, ident.AgentID, delta)
	if err != nil {
		slog.WarnContext(ctx, "impression: add score failed", "err", err.Error())
		return
	}
	threshold := resolveImpressionThreshold(resolver)
	if newScore >= threshold {
		if err := store.ResetScore(ctx, ident.AccountID, ident.UserID, ident.AgentID); err != nil {
			slog.WarnContext(ctx, "impression: reset score failed", "err", err.Error())
			return
		}
		if refresh != nil {
			refresh(ctx, ident, ident.AccountID, ident.UserID)
		}
	}
}

func resolveImpressionThreshold(resolver sharedconfig.Resolver) int {
	if resolver == nil {
		return 50
	}
	raw, err := resolver.Resolve(context.Background(), "memory.impression_score_threshold", sharedconfig.Scope{})
	if err != nil || strings.TrimSpace(raw) == "" {
		return 50
	}
	v, parseErr := strconv.Atoi(strings.TrimSpace(raw))
	if parseErr != nil || v <= 0 {
		return 50
	}
	return v
}
