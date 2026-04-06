package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pkoukk/tiktoken-go"
)

const defaultChannelGroupMaxContextTokens = 32768

// 群聊截断在 Routing 之前执行，没有选定模型；用 o200k 统一估算正文，与 HistoryThreadPromptTokens 默认回退一致。
// 每条里的图片单独加固定预算（PartPromptText 对 image 为空，不能仅靠正文 tiktoken）。
const groupTrimVisionTokensPerImage = 1024

const (
	defaultGroupCompactTriggerPct = 80
	defaultGroupCompactKeepPct    = 25
	defaultGroupKeepImageTail     = 10
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
	AuxGateway      llm.Gateway
	EmitDebugEvents bool
	ConfigLoader    *routing.ConfigLoader
}

// NewChannelGroupContextTrimMiddleware 在 Channel 群聊 Run 上按近似 token 预算裁剪 Thread 历史（保留时间轴尾部），
// 并在上下文压力达到阈值时执行群聊专用 compact。
func NewChannelGroupContextTrimMiddleware(deps ...GroupContextTrimDeps) RunMiddleware {
	maxTokens := defaultChannelGroupMaxContextTokens
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxTokens = n
		}
	}
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

		stripOlderImages(rc, keepImageTail)
		compactResult := maybeGroupCompact(ctx, rc, dep, maxTokens)
		beforeTrim := snapshotGroupTrimStats(rc)
		trimRunContextMessagesToApproxTokens(rc, maxTokens)
		trimEvent := buildGroupTrimEvent(beforeTrim, snapshotGroupTrimStats(rc), maxTokens, compactResult != nil)

		nextErr := next(ctx, rc)

		if trimEvent != nil && dep.EmitDebugEvents {
			postCtx, cancel := context.WithTimeout(context.Background(), contextCompactPostWriteTimeout)
			defer cancel()
			if err := appendContextCompactRunEvent(postCtx, dep.Pool, dep.EventsRepo, rc, trimEvent); err != nil {
				slog.WarnContext(ctx, "group_trim", "phase", "run_event", "err", err.Error(), "run_id", rc.Run.ID.String())
			}
		}

		// 持久化放在 next 之后，不阻塞 pipeline 主路径
		if compactResult != nil {
			persistGroupCompact(ctx, rc, dep, *compactResult)
		}

		return nextErr
	}
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

// groupCompactResult 存储 compact LLM 结果，供 next 之后持久化。
type groupCompactResult struct {
	PrefixIDs     []uuid.UUID
	Summary       string
	Split         int
	TotalTokens   int
	TriggerTokens int
	KeepCount     int
}

// maybeGroupCompact 双阈值群聊 compact：当消息 token 总量 >= maxTokens * 80% 时触发，
// 从尾部保留 maxTokens * 25% token 的真实消息，其余压入 snapshot。
// 返回非 nil 时表示需要在 next 之后持久化。
func maybeGroupCompact(ctx context.Context, rc *RunContext, dep GroupContextTrimDeps, maxTokens int) *groupCompactResult {
	if rc == nil || dep.Pool == nil || len(rc.Messages) < 3 {
		return nil
	}
	msgs := rc.Messages
	ids := rc.ThreadMessageIDs
	if len(ids) != len(msgs) {
		return nil
	}

	triggerTokens := maxTokens * defaultGroupCompactTriggerPct / 100
	keepTokens := maxTokens * defaultGroupCompactKeepPct / 100

	// 跳过 snapshot 前缀
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

	// 从尾部往前累加，确定保留区边界（以整条消息为单位）
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
	// 用 stabilizeCompactStart 确保不以孤立的 tool 消息开头
	split = stabilizeCompactStart(realMsgs, split, 0)
	split = ensureToolPairIntegrity(realMsgs, split)
	if split <= 0 {
		return nil
	}

	compactPrefix := realMsgs[:split]
	compactPrefixIDs := make([]uuid.UUID, split)
	copy(compactPrefixIDs, ids[fixedPrefix:fixedPrefix+split])

	previousSummary := ""
	if rc.HasActiveCompactSnapshot {
		previousSummary = strings.TrimSpace(rc.ActiveCompactSnapshotText)
	}

	gw, model := resolveGroupCompactGateway(ctx, dep, rc)
	if gw == nil {
		slog.WarnContext(ctx, "group_compact", "phase", "gateway_nil", "run_id", rc.Run.ID.String())
		return nil
	}

	// file lock 防止 Desktop 端并发 compact
	fileLockCleanup, fileLockErr := CompactThreadCompactionLock(ctx, rc.Run.ThreadID)
	if fileLockErr != nil {
		slog.WarnContext(ctx, "group_compact", "phase", "file_lock", "err", fileLockErr.Error(), "run_id", rc.Run.ID.String())
	}
	if fileLockCleanup != nil {
		defer fileLockCleanup()
	}

	enc := groupTrimEncoder()

	slog.InfoContext(ctx, "group_compact", "phase", "started",
		"run_id", rc.Run.ID.String(),
		"thread_id", rc.Run.ThreadID.String(),
		"total_tokens", totalTokens,
		"trigger_tokens", triggerTokens,
		"split", split,
		"keep_count", len(realMsgs)-split,
	)

	summary, err := runGroupCompactLLM(ctx, gw, model, compactPrefix, enc, previousSummary)
	if err != nil {
		slog.WarnContext(ctx, "group_compact", "phase", "llm_failed", "err", err.Error(), "run_id", rc.Run.ID.String())
		return nil
	}
	if strings.TrimSpace(summary) == "" {
		return nil
	}

	// 更新内存中的消息：snapshot + 保留区
	tail := make([]llm.Message, len(realMsgs)-split)
	copy(tail, realMsgs[split:])
	rc.Messages = append([]llm.Message{makeCompactSnapshotMessage(summary)}, tail...)
	rc.ThreadMessageIDs = append([]uuid.UUID{uuid.Nil}, ids[fixedPrefix+split:]...)
	rc.HasActiveCompactSnapshot = true
	rc.ActiveCompactSnapshotText = summary

	return &groupCompactResult{
		PrefixIDs:     compactPrefixIDs,
		Summary:       summary,
		Split:         split,
		TotalTokens:   totalTokens,
		TriggerTokens: triggerTokens,
		KeepCount:     len(realMsgs) - split,
	}
}

func sumMessageTokens(msgs []llm.Message) int {
	total := 0
	for i := range msgs {
		total += messageTokens(&msgs[i])
	}
	return total
}

// resolveGroupCompactGateway 为群聊 compact 解析 LLM gateway。
// Channel 层在 Routing 之前执行，rc.Gateway 还未设置，通过 AuxGateway + ConfigLoader 解析。
func resolveGroupCompactGateway(ctx context.Context, dep GroupContextTrimDeps, rc *RunContext) (llm.Gateway, string) {
	if dep.Pool == nil || dep.AuxGateway == nil {
		return nil, ""
	}

	// 先尝试 account 级别的 tool gateway
	fallbackGw := dep.AuxGateway
	fallbackModel := ""
	if gw, model, ok := resolveAccountToolGateway(ctx, dep.Pool, rc.Run.AccountID, dep.AuxGateway, dep.EmitDebugEvents, rc.LlmMaxResponseBytes, dep.ConfigLoader, rc.RoutingByokEnabled); ok {
		fallbackGw = gw
		fallbackModel = model
	}

	// 尝试 platform_settings 中的 context.compaction.model
	var selector string
	err := dep.Pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`,
		settingContextCompactionModel,
	).Scan(&selector)
	selector = strings.TrimSpace(selector)
	if err != nil || selector == "" || dep.ConfigLoader == nil {
		return fallbackGw, fallbackModel
	}
	aid := rc.Run.AccountID
	routingCfg, err := dep.ConfigLoader.Load(ctx, &aid)
	if err != nil {
		return fallbackGw, fallbackModel
	}
	selected, err := resolveSelectedRouteBySelector(routingCfg, selector, map[string]any{}, rc.RoutingByokEnabled)
	if err != nil || selected == nil {
		return fallbackGw, fallbackModel
	}
	gw, err := gatewayFromSelectedRoute(*selected, dep.AuxGateway, dep.EmitDebugEvents, rc.LlmMaxResponseBytes)
	if err != nil {
		return fallbackGw, fallbackModel
	}
	return gw, selected.Route.Model
}

// persistGroupCompact 将群聊 compact 结果持久化到数据库。在 next 之后调用，不阻塞 pipeline 主路径。
func persistGroupCompact(
	ctx context.Context,
	rc *RunContext,
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
		slog.WarnContext(ctx, "group_compact", "phase", "tx_begin", "err", txErr.Error(), "run_id", rc.Run.ID.String())
		return
	}

	if lockErr := compactThreadCompactionAdvisoryXactLock(postCtx, tx, rc.Run.ThreadID); lockErr != nil {
		_ = tx.Rollback(postCtx)
		emitContextCompactFailure(ctx, postCtx, dep.Pool, dep.EventsRepo, rc, "group_persist", "advisory_lock", lockErr)
		slog.WarnContext(ctx, "group_compact", "phase", "advisory_lock", "err", lockErr.Error(), "run_id", rc.Run.ID.String())
		return
	}

	if len(filteredIDs) > 0 {
		still, chkErr := compactPrefixMessagesStillUncompacted(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, filteredIDs)
		if chkErr != nil {
			_ = tx.Rollback(postCtx)
			emitContextCompactFailure(ctx, postCtx, dep.Pool, dep.EventsRepo, rc, "group_persist", "prefix_precheck", chkErr)
			slog.WarnContext(ctx, "group_compact", "phase", "prefix_precheck", "err", chkErr.Error(), "run_id", rc.Run.ID.String())
			return
		}
		if !still {
			_ = tx.Rollback(postCtx)
			return
		}
		if err := dep.MessagesRepo.MarkThreadMessagesCompacted(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, filteredIDs); err != nil {
			_ = tx.Rollback(postCtx)
			emitContextCompactFailure(ctx, postCtx, dep.Pool, dep.EventsRepo, rc, "group_persist", "mark_compacted", err)
			slog.WarnContext(ctx, "group_compact", "phase", "mark_compacted", "err", err.Error(), "run_id", rc.Run.ID.String())
			return
		}
	}

	meta, _ := json.Marshal(map[string]string{"kind": "group_context_compact"})
	_, insErr := (data.ThreadCompactionSnapshotsRepository{}).ReplaceActive(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, result.Summary, meta)
	if insErr != nil {
		_ = tx.Rollback(postCtx)
		emitContextCompactFailure(ctx, postCtx, dep.Pool, dep.EventsRepo, rc, "group_persist", "replace_snapshot", insErr)
		slog.WarnContext(ctx, "group_compact", "phase", "replace_snapshot", "err", insErr.Error(), "run_id", rc.Run.ID.String())
		return
	}

	if dep.EventsRepo != nil {
		ev := rc.Emitter.Emit("run.context_compact", map[string]any{
			"op":             "group_persist",
			"phase":          "completed",
			"persist_split":  result.Split,
			"total_tokens":   result.TotalTokens,
			"trigger_tokens": result.TriggerTokens,
			"keep_count":     result.KeepCount,
		}, nil, nil)
		if _, evErr := dep.EventsRepo.AppendRunEvent(postCtx, tx, rc.Run.ID, ev); evErr != nil {
			_ = tx.Rollback(postCtx)
			slog.WarnContext(ctx, "group_compact", "phase", "run_event", "err", evErr.Error(), "run_id", rc.Run.ID.String())
			return
		}
	}

	if err := tx.Commit(postCtx); err != nil {
		slog.WarnContext(ctx, "group_compact", "phase", "tx_commit", "err", err.Error(), "run_id", rc.Run.ID.String())
		return
	}

	slog.InfoContext(ctx, "group_compact", "phase", "persisted",
		"run_id", rc.Run.ID.String(),
		"thread_id", rc.Run.ThreadID.String(),
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
