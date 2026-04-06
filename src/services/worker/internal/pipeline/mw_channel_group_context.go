package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pkoukk/tiktoken-go"
)

const defaultChannelGroupMaxContextTokens = 32768

const groupTrimVisionTokensPerImage = 1024

const (
	defaultGroupCompactTriggerPct = 80
	defaultGroupCompactKeepPct    = 25
	defaultGroupKeepImageTail     = 10

	maxGroupCompactRetries       = 2
	maxGroupCompactPrefixShrinks = 3
	groupCompactSummaryMaxRunes  = 40000 // ~10k tokens
)

var (
	groupTrimEncOnce sync.Once
	groupTrimEnc     *tiktoken.Tiktoken
	groupTrimEncErr  error
)

func groupTrimEncoder() *tiktoken.Tiktoken {
	groupTrimEncOnce.Do(func() {
		groupTrimEnc, groupTrimEncErr = tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	})
	if groupTrimEncErr != nil {
		return nil
	}
	return groupTrimEnc
}

// GroupContextTrimDeps 群聊截断 + compact 所需的依赖。
type GroupContextTrimDeps struct {
	Pool            CompactPersistDB
	MessagesRepo    data.MessagesRepository
	EventsRepo      CompactRunEventAppender
	EmitDebugEvents bool
}

// NewChannelGroupContextTrimMiddleware 在 Routing 之后运行，按近似 token 预算裁剪群聊 Thread 历史，
// 并在上下文压力达到阈值时异步执行群聊专用 compact。
func NewChannelGroupContextTrimMiddleware(deps ...GroupContextTrimDeps) RunMiddleware {
	keepImageTail := defaultGroupKeepImageTail
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_GROUP_KEEP_IMAGE_TAIL")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			keepImageTail = n
		}
	}

	var dep GroupContextTrimDeps
	if len(deps) > 0 {
		dep = deps[0]
	}

	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}

		projectGroupEnvelopes(rc)

		if !IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
			return next(ctx, rc)
		}

		maxTokens := resolveGroupMaxTokens(rc)

		stripOlderImages(rc, keepImageTail)
		compactParams := shouldGroupCompact(ctx, rc, dep, maxTokens)
		beforeTrim := snapshotGroupTrimStats(rc)
		trimRunContextMessagesToApproxTokens(rc, maxTokens)
		trimEvent := buildGroupTrimEvent(beforeTrim, snapshotGroupTrimStats(rc), maxTokens, compactParams != nil)

		nextErr := next(ctx, rc)

		if trimEvent != nil && dep.EmitDebugEvents {
			postCtx, cancel := context.WithTimeout(context.Background(), contextCompactPostWriteTimeout)
			defer cancel()
			if err := appendContextCompactRunEvent(postCtx, dep.Pool, dep.EventsRepo, rc, trimEvent); err != nil {
				slog.WarnContext(ctx, "group_trim", "phase", "run_event", "err", err.Error(), "run_id", rc.Run.ID.String())
			}
		}

		if compactParams != nil {
			runGroupCompactAsync(ctx, rc, dep, *compactParams)
		}

		return nextErr
	}
}

// resolveGroupMaxTokens 动态计算群聊 maxTokens：
// 优先从 SelectedRoute 的 context window 按比例计算，fallback 到环境变量或硬编码值。
func resolveGroupMaxTokens(rc *RunContext) int {
	if rc.SelectedRoute != nil {
		window := routing.RouteContextWindowTokens(rc.SelectedRoute.Route)
		if window > 0 {
			return window * defaultGroupCompactTriggerPct / 100
		}
	}
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return defaultChannelGroupMaxContextTokens
}

// stripOlderImages 将尾部 keepTail 条之前的消息中的图片替换为带 attachment_key 的占位符。
func stripOlderImages(rc *RunContext, keepTail int) {
	if rc == nil || len(rc.Messages) == 0 || keepTail < 0 {
		return
	}
	boundary := len(rc.Messages) - keepTail
	if boundary <= 0 {
		return
	}
	for i := 0; i < boundary; i++ {
		replaced := false
		parts := rc.Messages[i].Content
		for j := range parts {
			if parts[j].Kind() == messagecontent.PartTypeImage {
				tag := "[image]"
				if parts[j].Attachment != nil && parts[j].Attachment.Key != "" {
					tag = "[image attachment_key=" + strconv.Quote(parts[j].Attachment.Key) + "]"
				}
				parts[j] = llm.ContentPart{
					Type: messagecontent.PartTypeText,
					Text: tag,
				}
				replaced = true
			}
		}
		if replaced {
			rc.Messages[i].Content = parts
		}
	}
}

// groupCompactParams 存储 compact 所需的参数，供异步 goroutine 使用。
type groupCompactParams struct {
	PrefixMsgs      []llm.Message
	PrefixIDs       []uuid.UUID
	PreviousSummary string
	Gateway         llm.Gateway
	Model           string
	Split           int
	TotalTokens     int
	TriggerTokens   int
	KeepCount       int
}

// shouldGroupCompact 同步判断是否需要 compact，返回 compact 参数（不调用 LLM）。
func shouldGroupCompact(ctx context.Context, rc *RunContext, dep GroupContextTrimDeps, maxTokens int) *groupCompactParams {
	if rc == nil || dep.Pool == nil || len(rc.Messages) < 3 {
		return nil
	}
	msgs := rc.Messages
	ids := rc.ThreadMessageIDs
	if len(ids) != len(msgs) {
		return nil
	}

	// 断路器
	if compactConsecutiveFailures(ctx, dep.Pool, rc.Run.AccountID, rc.Run.ThreadID) >= maxConsecutiveCompactFailures {
		slog.InfoContext(ctx, "group_compact", "phase", "circuit_breaker", "run_id", rc.Run.ID.String(), "thread_id", rc.Run.ThreadID.String())
		return nil
	}

	triggerTokens := maxTokens * defaultGroupCompactTriggerPct / 100
	keepTokens := maxTokens * defaultGroupCompactKeepPct / 100

	fixedPrefix := 0
	if rc.HasActiveCompactSnapshot && len(msgs) > 0 && len(ids) > 0 && ids[0] == uuid.Nil {
		fixedPrefix = 1
	}
	realMsgs := msgs[fixedPrefix:]
	if len(realMsgs) < 2 {
		return nil
	}

	totalTokens := sumMessageTokens(realMsgs)
	if totalTokens < triggerTokens {
		return nil
	}

	keepAccum := 0
	split := len(realMsgs)
	for i := len(realMsgs) - 1; i >= 0; i-- {
		t := messageTokens(&realMsgs[i])
		if keepAccum+t > keepTokens && i < len(realMsgs)-1 {
			split = i + 1
			break
		}
		keepAccum += t
		if i == 0 {
			split = 0
		}
	}
	split = stabilizeCompactStart(realMsgs, split, 0)
	split = ensureToolPairIntegrity(realMsgs, split)
	if split <= 0 {
		return nil
	}

	previousSummary := ""
	if rc.HasActiveCompactSnapshot {
		previousSummary = strings.TrimSpace(rc.ActiveCompactSnapshotText)
	}

	gw, model := resolveGroupCompactGateway(ctx, dep, rc)
	if gw == nil {
		slog.WarnContext(ctx, "group_compact", "phase", "gateway_nil", "run_id", rc.Run.ID.String())
		return nil
	}

	prefixMsgs := make([]llm.Message, split)
	copy(prefixMsgs, realMsgs[:split])
	prefixIDs := make([]uuid.UUID, split)
	copy(prefixIDs, ids[fixedPrefix:fixedPrefix+split])

	return &groupCompactParams{
		PrefixMsgs:      prefixMsgs,
		PrefixIDs:       prefixIDs,
		PreviousSummary: previousSummary,
		Gateway:         gw,
		Model:           model,
		Split:           split,
		TotalTokens:     totalTokens,
		TriggerTokens:   triggerTokens,
		KeepCount:       len(realMsgs) - split,
	}
}

// runGroupCompactAsync 异步执行群聊 compact LLM 调用 + 持久化。
func runGroupCompactAsync(parentCtx context.Context, rc *RunContext, dep GroupContextTrimDeps, params groupCompactParams) {
	// 快照 rc 中持久化所需的不可变数据
	runID := rc.Run.ID
	threadID := rc.Run.ThreadID
	accountID := rc.Run.AccountID
	emitter := rc.Emitter

	go func() {
		ctx := context.WithoutCancel(parentCtx)

		slog.InfoContext(ctx, "group_compact", "phase", "async_started",
			"run_id", runID.String(),
			"thread_id", threadID.String(),
			"total_tokens", params.TotalTokens,
			"trigger_tokens", params.TriggerTokens,
			"split", params.Split,
			"keep_count", params.KeepCount,
		)

		fileLockCleanup, fileLockErr := CompactThreadCompactionLock(ctx, threadID)
		if fileLockErr != nil {
			slog.WarnContext(ctx, "group_compact", "phase", "file_lock", "err", fileLockErr.Error(), "run_id", runID.String())
		}
		if fileLockCleanup != nil {
			defer fileLockCleanup()
		}

		enc := groupTrimEncoder()
		summary := runGroupCompactWithRetry(ctx, params.Gateway, params.Model, params.PrefixMsgs, enc, params.PreviousSummary)

		if summary == "" {
			emitGroupCompactFailure(ctx, dep, runID, accountID, threadID, emitter)
			return
		}

		summary = truncateGroupSummary(summary)

		result := groupCompactResult{
			PrefixIDs:     params.PrefixIDs,
			Summary:       summary,
			Split:         params.Split,
			TotalTokens:   params.TotalTokens,
			TriggerTokens: params.TriggerTokens,
			KeepCount:     params.KeepCount,
		}
		persistGroupCompact(ctx, runID, threadID, accountID, emitter, dep, result)
	}()
}

// runGroupCompactWithRetry 带重试的群聊 compact LLM 调用。
func runGroupCompactWithRetry(ctx context.Context, gw llm.Gateway, model string, prefix []llm.Message, enc *tiktoken.Tiktoken, previousSummary string) string {
	shrinkAttempts := 0
	for attempt := 0; attempt <= maxGroupCompactRetries; attempt++ {
		summary, err := runGroupCompactLLM(ctx, gw, model, prefix, enc, previousSummary)
		if err == nil && strings.TrimSpace(summary) != "" {
			return summary
		}
		if err == nil {
			return ""
		}

		errMsg := strings.ToLower(err.Error())
		if isContextWindowExceeded(errMsg) && shrinkAttempts < maxGroupCompactPrefixShrinks && len(prefix) > 1 {
			prefix = prefix[1:]
			shrinkAttempts++
			attempt--
			slog.WarnContext(ctx, "group_compact", "phase", "shrink_prefix", "remaining", len(prefix), "shrink_attempt", shrinkAttempts)
			continue
		}

		slog.WarnContext(ctx, "group_compact", "phase", "llm_retry", "attempt", attempt, "err", err.Error())
		if attempt < maxGroupCompactRetries {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
	}
	return ""
}

func isContextWindowExceeded(errMsg string) bool {
	for _, kw := range []string{"context_length_exceeded", "max_tokens", "too many tokens", "maximum context length", "token limit"} {
		if strings.Contains(errMsg, kw) {
			return true
		}
	}
	return false
}

// truncateGroupSummary 防御性截断：超过上限时按 ## section 边界截断。
func truncateGroupSummary(summary string) string {
	runes := []rune(summary)
	if len(runes) <= groupCompactSummaryMaxRunes {
		return summary
	}
	truncated := string(runes[:groupCompactSummaryMaxRunes])
	if idx := strings.LastIndex(truncated, "\n## "); idx > 0 {
		truncated = truncated[:idx]
	}
	return truncated
}

func emitGroupCompactFailure(ctx context.Context, dep GroupContextTrimDeps, runID, accountID, threadID uuid.UUID, emitter events.Emitter) {
	if dep.Pool == nil || dep.EventsRepo == nil {
		return
	}
	postCtx, cancel := context.WithTimeout(ctx, contextCompactPostWriteTimeout)
	defer cancel()
	tx, txErr := dep.Pool.BeginTx(postCtx, pgx.TxOptions{})
	if txErr != nil {
		return
	}
	ev := emitter.Emit("run.context_compact", map[string]any{
		"op":    "group_persist",
		"phase": "failed",
	}, nil, nil)
	if _, err := dep.EventsRepo.AppendRunEvent(postCtx, tx, runID, ev); err != nil {
		_ = tx.Rollback(postCtx)
		slog.WarnContext(ctx, "group_compact", "phase", "emit_failure_event", "err", err.Error(), "run_id", runID.String())
		return
	}
	_ = tx.Commit(postCtx)
}

func sumMessageTokens(msgs []llm.Message) int {
	total := 0
	for i := range msgs {
		total += messageTokens(&msgs[i])
	}
	return total
}

// resolveGroupCompactGateway 在 Routing 之后执行，优先使用 rc.Gateway，
// 其次查 platform_settings.context.compaction.model。
func resolveGroupCompactGateway(ctx context.Context, dep GroupContextTrimDeps, rc *RunContext) (llm.Gateway, string) {
	if rc.Gateway == nil {
		return nil, ""
	}
	fallbackGw := rc.Gateway
	fallbackModel := ""
	if rc.SelectedRoute != nil {
		fallbackModel = rc.SelectedRoute.Route.Model
	}

	if dep.Pool == nil {
		return fallbackGw, fallbackModel
	}

	var selector string
	err := dep.Pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`,
		settingContextCompactionModel,
	).Scan(&selector)
	selector = strings.TrimSpace(selector)
	if err != nil || selector == "" {
		return fallbackGw, fallbackModel
	}
	// 使用 resolveCompactionGateway 的模式：通过 selector 解析专用 compact 路由
	if rc.ResolveGatewayForRouteID != nil {
		gw, selected, rerr := rc.ResolveGatewayForRouteID(ctx, selector)
		if rerr == nil && gw != nil && selected != nil {
			return gw, selected.Route.Model
		}
	}
	return fallbackGw, fallbackModel
}

// groupCompactResult 存储 compact 结果，供持久化使用。
type groupCompactResult struct {
	PrefixIDs     []uuid.UUID
	Summary       string
	Split         int
	TotalTokens   int
	TriggerTokens int
	KeepCount     int
}

// persistGroupCompact 将群聊 compact 结果持久化到数据库。
func persistGroupCompact(
	ctx context.Context,
	runID, threadID, accountID uuid.UUID,
	emitter events.Emitter,
	dep GroupContextTrimDeps,
	result groupCompactResult,
) {
	if dep.Pool == nil {
		return
	}
	filteredIDs := filterNonNilUUIDs(result.PrefixIDs)

	postCtx, cancel := context.WithTimeout(context.Background(), contextCompactPostWriteTimeout)
	defer cancel()

	tx, txErr := dep.Pool.BeginTx(postCtx, pgx.TxOptions{})
	if txErr != nil {
		slog.WarnContext(ctx, "group_compact", "phase", "tx_begin", "err", txErr.Error(), "run_id", runID.String())
		return
	}

	if lockErr := compactThreadCompactionAdvisoryXactLock(postCtx, tx, threadID); lockErr != nil {
		_ = tx.Rollback(postCtx)
		slog.WarnContext(ctx, "group_compact", "phase", "advisory_lock", "err", lockErr.Error(), "run_id", runID.String())
		return
	}

	if len(filteredIDs) > 0 {
		still, chkErr := compactPrefixMessagesStillUncompacted(postCtx, tx, accountID, threadID, filteredIDs)
		if chkErr != nil {
			_ = tx.Rollback(postCtx)
			slog.WarnContext(ctx, "group_compact", "phase", "prefix_precheck", "err", chkErr.Error(), "run_id", runID.String())
			return
		}
		if !still {
			_ = tx.Rollback(postCtx)
			return
		}
		if err := dep.MessagesRepo.MarkThreadMessagesCompacted(postCtx, tx, accountID, threadID, filteredIDs); err != nil {
			_ = tx.Rollback(postCtx)
			slog.WarnContext(ctx, "group_compact", "phase", "mark_compacted", "err", err.Error(), "run_id", runID.String())
			return
		}
	}

	meta, _ := json.Marshal(map[string]string{"kind": "group_context_compact"})
	_, insErr := (data.ThreadCompactionSnapshotsRepository{}).ReplaceActive(postCtx, tx, accountID, threadID, result.Summary, meta)
	if insErr != nil {
		_ = tx.Rollback(postCtx)
		slog.WarnContext(ctx, "group_compact", "phase", "replace_snapshot", "err", insErr.Error(), "run_id", runID.String())
		return
	}

	if dep.EventsRepo != nil {
		ev := emitter.Emit("run.context_compact", map[string]any{
			"op":             "group_persist",
			"phase":          "completed",
			"persist_split":  result.Split,
			"total_tokens":   result.TotalTokens,
			"trigger_tokens": result.TriggerTokens,
			"keep_count":     result.KeepCount,
		}, nil, nil)
		if _, evErr := dep.EventsRepo.AppendRunEvent(postCtx, tx, runID, ev); evErr != nil {
			_ = tx.Rollback(postCtx)
			slog.WarnContext(ctx, "group_compact", "phase", "run_event", "err", evErr.Error(), "run_id", runID.String())
			return
		}
	}

	if err := tx.Commit(postCtx); err != nil {
		slog.WarnContext(ctx, "group_compact", "phase", "tx_commit", "err", err.Error(), "run_id", runID.String())
		return
	}

	slog.InfoContext(ctx, "group_compact", "phase", "persisted",
		"run_id", runID.String(),
		"thread_id", threadID.String(),
		"compacted_messages", len(filteredIDs),
		"keep_count", result.KeepCount,
	)
}

// IsTelegramGroupLikeConversation 判断 Telegram 侧群 / 超级群 / 频道（非私信）。
func IsTelegramGroupLikeConversation(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}

type groupTrimStats struct {
	MessageCount         int
	RealMessageCount     int
	HasSnapshotPrefix    bool
	EstimatedTrimWeight  int
	EstimatedTextTokens  int
	EstimatedImageTokens int
}

func snapshotGroupTrimStats(rc *RunContext) groupTrimStats {
	if rc == nil || len(rc.Messages) == 0 {
		return groupTrimStats{}
	}
	msgs := rc.Messages
	ids := rc.ThreadMessageIDs
	alignedIDs := len(ids) == len(msgs)
	hasSnapshot := rc.HasActiveCompactSnapshot && len(msgs) > 1 && alignedIDs && ids[0] == uuid.Nil
	realStart := 0
	if hasSnapshot {
		realStart = 1
	}
	stats := groupTrimStats{
		MessageCount:      len(msgs),
		RealMessageCount:  len(msgs) - realStart,
		HasSnapshotPrefix: hasSnapshot,
	}
	for i := range msgs {
		stats.EstimatedTrimWeight += messageTokens(&msgs[i])
		stats.EstimatedTextTokens += approxLLMMessageTextTokens(msgs[i])
		stats.EstimatedImageTokens += approxLLMMessageImageTokens(msgs[i])
	}
	return stats
}

func buildGroupTrimEvent(before, after groupTrimStats, maxTokens int, compactTriggered bool) map[string]any {
	if before.RealMessageCount <= after.RealMessageCount &&
		before.EstimatedTrimWeight == after.EstimatedTrimWeight {
		return nil
	}
	droppedCount := before.RealMessageCount - after.RealMessageCount
	if droppedCount < 0 {
		droppedCount = 0
	}
	return map[string]any{
		"op":                            "group_trim",
		"phase":                         "completed",
		"max_tokens":                    maxTokens,
		"messages_before":               before.MessageCount,
		"messages_after":                after.MessageCount,
		"kept_count":                    after.RealMessageCount,
		"dropped_count":                 droppedCount,
		"has_snapshot_prefix":           before.HasSnapshotPrefix,
		"compact_triggered":             compactTriggered,
		"estimated_trim_weight_before":  before.EstimatedTrimWeight,
		"estimated_trim_weight_after":   after.EstimatedTrimWeight,
		"estimated_text_tokens_before":  before.EstimatedTextTokens,
		"estimated_text_tokens_after":   after.EstimatedTextTokens,
		"estimated_image_tokens_before": before.EstimatedImageTokens,
		"estimated_image_tokens_after":  after.EstimatedImageTokens,
	}
}

// trimRunContextMessagesToApproxTokens snapshot 感知版本：如果头部是 compact snapshot，
// 先预留其 token 开销，剩余预算从尾部保留真实消息，最后 prepend snapshot。
func trimRunContextMessagesToApproxTokens(rc *RunContext, maxTokens int) {
	if rc == nil || maxTokens <= 0 || len(rc.Messages) == 0 {
		return
	}
	msgs := rc.Messages
	ids := rc.ThreadMessageIDs
	alignedIDs := len(ids) == len(msgs)

	// 检测头部是否为 snapshot
	hasSnapshot := rc.HasActiveCompactSnapshot && len(msgs) > 1 && alignedIDs && ids[0] == uuid.Nil
	snapshotTokens := 0
	realStart := 0
	if hasSnapshot {
		snapshotTokens = messageTokens(&msgs[0])
		realStart = 1
	}

	budget := maxTokens - snapshotTokens
	if budget <= 0 {
		// snapshot 本身就超预算，只保留 snapshot
		if hasSnapshot {
			rc.Messages = msgs[:1]
			if alignedIDs {
				rc.ThreadMessageIDs = ids[:1]
			}
		}
		return
	}

	realMsgs := msgs[realStart:]
	total := 0
	start := len(realMsgs)
	for i := len(realMsgs) - 1; i >= 0; i-- {
		t := messageTokens(&realMsgs[i])
		if total+t > budget {
			break
		}
		total += t
		start = i
	}

	start = ensureToolPairIntegrity(realMsgs, start)

	if start <= 0 && !hasSnapshot {
		return
	}

	if hasSnapshot {
		kept := realMsgs[start:]
		rc.Messages = make([]llm.Message, 0, 1+len(kept))
		rc.Messages = append(rc.Messages, msgs[0])
		rc.Messages = append(rc.Messages, kept...)
		if alignedIDs {
			keptIDs := ids[realStart+start:]
			rc.ThreadMessageIDs = make([]uuid.UUID, 0, 1+len(keptIDs))
			rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, ids[0])
			rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, keptIDs...)
		}
	} else {
		if start >= len(realMsgs) {
			return
		}
		rc.Messages = msgs[start:]
		if alignedIDs {
			rc.ThreadMessageIDs = ids[start:]
		}
	}
}

// messageTokens 估算单条在历史截断里的权重，顺序：
// 1) assistant 且 usage_records.output_tokens>0（模型侧真实 completion，Desktop 与 Postgres 的 ListByThread 均已 JOIN）
// 2) tiktoken o200k 估正文+tool；图按固定 vision 预算
// 3) tiktoken 初始化失败则 legacy：rune/3
//
// user 等角色不能用 output_tokens：metadata 里同 run_id 会 JOIN 到同一条 usage，数值语义不是「本条 user 长度」。
func messageTokens(m *llm.Message) int {
	if m != nil && strings.EqualFold(strings.TrimSpace(m.Role), "assistant") &&
		m.OutputTokens != nil && *m.OutputTokens > 0 {
		return int(*m.OutputTokens)
	}
	if m == nil {
		return 1
	}
	return approxLLMMessageTokens(*m)
}

func approxLLMMessageTokens(m llm.Message) int {
	enc := groupTrimEncoder()
	if enc == nil {
		return approxLLMMessageTokensLegacy(m)
	}
	n := approxLLMMessageTextTokensWithEncoder(enc, m)
	n += approxLLMMessageImageTokens(m)
	if n < 1 {
		return 1
	}
	return n
}

func approxLLMMessageTextTokens(m llm.Message) int {
	enc := groupTrimEncoder()
	if enc == nil {
		return approxLLMMessageTextTokensLegacy(m)
	}
	return approxLLMMessageTextTokensWithEncoder(enc, m)
}

func approxLLMMessageTextTokensWithEncoder(enc *tiktoken.Tiktoken, m llm.Message) int {
	const tokensPerMessage = 3
	n := tokensPerMessage
	n += len(enc.Encode(m.Role, nil, nil))
	body := messageText(m)
	for _, tc := range m.ToolCalls {
		body += "\n"
		body += tc.ToolName
		if b, err := json.Marshal(tc.ArgumentsJSON); err == nil {
			body += string(b)
		}
	}
	n += len(enc.Encode(body, nil, nil))
	if n < 1 {
		return 1
	}
	return n
}

func approxLLMMessageImageTokens(m llm.Message) int {
	total := 0
	for _, p := range m.Content {
		if p.Kind() == messagecontent.PartTypeImage {
			total += groupTrimVisionTokensPerImage
		}
	}
	return total
}

func approxLLMMessageTokensLegacy(m llm.Message) int {
	n := approxLLMMessageTextTokensLegacy(m)
	n += approxLLMMessageImageTokensLegacy(m)
	if n < 1 {
		return 1
	}
	return n
}

func approxLLMMessageTextTokensLegacy(m llm.Message) int {
	n := 0
	for _, p := range m.Content {
		n += utf8.RuneCountInString(p.Text)
		n += utf8.RuneCountInString(p.ExtractedText)
		if p.Attachment != nil {
			n += 64
		}
		if len(p.Data) > 0 && p.Kind() != messagecontent.PartTypeImage {
			raw := len(p.Data) / 4
			n += raw
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

func approxLLMMessageImageTokensLegacy(m llm.Message) int {
	total := 0
	for _, p := range m.Content {
		if p.Kind() != messagecontent.PartTypeImage {
			continue
		}
		raw := len(p.Data) / 4
		if raw > 3072 {
			raw = 3072
		}
		total += raw / 3
		if total < 1 {
			total = 1
		}
	}
	return total
}
