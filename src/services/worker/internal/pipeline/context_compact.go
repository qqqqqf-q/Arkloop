package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pkoukk/tiktoken-go"
)

const defaultFallbackContextWindowTokens = 128000

// ContextCompactSettings 来自平台配置，供 ContextCompactMiddleware 使用。
type ContextCompactSettings struct {
	Enabled bool

	// MaxMessages 尾部最多保留多少条消息；0 表示不按条数收缩。
	MaxMessages int

	// MaxUserMessageTokens 尾部 user 消息的 tiktoken 上限（SuffixRoleAndContentTokens 口径）；0 表示不限制。
	MaxUserMessageTokens int
	MaxTotalTextTokens   int
	MaxUserTextBytes     int
	MaxTotalTextBytes    int

	PersistEnabled             bool
	PersistTriggerApproxTokens int
	// PersistTriggerContextPct 1–100：按「上下文窗口」比例计算触发阈值；0 表示仅用 PersistTriggerApproxTokens。
	PersistTriggerContextPct int
	// TargetContextPct 1-100：compact 后尝试压回到上下文窗口的百分比；0 默认 75。
	TargetContextPct int
	// FallbackContextWindowTokens 路由无 available_catalog.context_length 时用于比例换算。
	FallbackContextWindowTokens int
	PersistKeepLastMessages     int
	// PersistKeepTailPct 1–100：persist 时保留 context window 的百分比作为尾部 token 预算；0 = 用旧的条数逻辑。
	PersistKeepTailPct int

	// CompactZoneBudgetPct 1–100：compact zone 最多占上下文窗口的百分比；超过时仅合并旧 replacement；0 默认 25。
	CompactZoneBudgetPct int

	// MicrocompactKeepRecentTools 保留最近 N 个 tool result 原文；0 = 不做 microcompact。
	MicrocompactKeepRecentTools int
}

type OversizeRewriteStats struct {
	RewriteApplied             bool
	ImagesStripped             int
	ToolResultsMicrocompacted  int
	CompactApplied             bool
	TargetChunkCount           int
	PreviousReplacementCount   int
	SingleAtomPartial          bool
	ContextWindowTokens        int
	RequestTokensBeforeRewrite int
	RequestTokensAfterRewrite  int
	MinimalRequestTokens       int
	RequestBytesBeforeRewrite  int
	RequestBytesAfterRewrite   int
	MinimalRequestBytes        int
	CurrentInputTooLarge       bool
}

type CurrentInputOversizeError struct {
	CurrentRequestEstimate int
	MinimalRequestEstimate int
	CurrentRequestTokens   int
	MinimalRequestTokens   int
	ContextWindowTokens    int
}

func (e *CurrentInputOversizeError) Error() string {
	if e == nil {
		return "current input node exceeds request limit"
	}
	return fmt.Sprintf(
		"current input node exceeds request limit: current=%d minimal=%d current_tokens=%d minimal_tokens=%d context_window=%d",
		e.CurrentRequestEstimate,
		e.MinimalRequestEstimate,
		e.CurrentRequestTokens,
		e.MinimalRequestTokens,
		e.ContextWindowTokens,
	)
}

func IsCurrentInputOversizeError(err error) (*CurrentInputOversizeError, bool) {
	if err == nil {
		return nil, false
	}
	var typed *CurrentInputOversizeError
	if !errors.As(err, &typed) {
		return nil, false
	}
	return typed, true
}

// 按字符区分估算 token 数：1 个英文字符 ≈ 0.3 token，1 个中文字符 ≈ 0.6 token
func approxTokensFromText(s string) int {
	n := len(s)
	if n == 0 {
		return 0
	}
	asciiChars := 0
	nonAsciiChars := 0
	for i := 0; i < n; {
		if s[i] < 0x80 {
			asciiChars++
			i++
		} else {
			nonAsciiChars++
			// 跳过 UTF-8 多字节序列
			if s[i]&0xE0 == 0xC0 {
				i += 2
			} else if s[i]&0xF0 == 0xE0 {
				i += 3
			} else {
				i += 4
			}
		}
	}
	tokens := int(float64(asciiChars)*0.3 + float64(nonAsciiChars)*0.6)
	if tokens < 1 {
		return 1
	}
	return tokens
}

func messageText(m llm.Message) string {
	var b strings.Builder
	for _, p := range m.Content {
		b.WriteString(llm.PartPromptText(p))
	}
	return b.String()
}

func countUserTokens(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role == "user" {
			n += approxTokensFromText(messageText(msgs[i]))
		}
	}
	return n
}

func countTotalTokens(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		n += approxTokensFromText(messageText(msgs[i]))
	}
	return n
}

func countUserBytes(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role == "user" {
			n += len(messageText(msgs[i]))
		}
	}
	return n
}

func countTotalBytes(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		n += len(messageText(msgs[i]))
	}
	return n
}

// stabilizeCompactStart 在「尾部条数上限」与「不以孤立 tool / assistant 开头」之间收敛切口。
// Anthropic 要求对话以 user 开头，因此切口跳过 tool 和 assistant 直到遇到 user。
func stabilizeCompactStart(msgs []llm.Message, start int, maxMessages int) int {
	if len(msgs) == 0 {
		return 0
	}
	maxIter := len(msgs) + 8
	for iter := 0; iter < maxIter; iter++ {
		for start > 0 && start < len(msgs) && msgs[start].Role == "tool" {
			start--
		}
		if maxMessages <= 0 || len(msgs)-start <= maxMessages {
			break
		}
		start++
		if start >= len(msgs) {
			start = len(msgs) - 1
			break
		}
	}
	for start < len(msgs)-1 && msgs[start].Role != "user" {
		start++
	}
	return start
}

// ensureToolPairIntegrity 确保 msgs[start:] 不以孤立 tool_result 开头。
// 如果 start 处是 role="tool"，向前扩展直到遇到非 tool 消息。
func ensureToolPairIntegrity(msgs []llm.Message, start int) int {
	for start > 0 && start < len(msgs) && msgs[start].Role == "tool" {
		start--
	}
	return start
}

// sanitizeToolPairs 扫描消息序列，移除不成对的 tool_use / tool_result。
// 孤立 tool: tool_call_id 在前一个 assistant 的 ToolCalls 中找不到匹配。
// 孤立 assistant(tool_use): 其所有 ToolCalls 对应的 tool 消息都不存在或已被移除。
// ids 若与 msgs 等长则同步裁切。
func sanitizeToolPairs(msgs []llm.Message, ids []uuid.UUID) ([]llm.Message, []uuid.UUID) {
	if len(msgs) == 0 {
		return msgs, ids
	}

	removeSet := make(map[int]struct{})
	prunedToolCalls := make(map[int][]llm.ToolCall)

	// pass 1: 标记孤立 tool 消息
	var activeCallIDs map[string]struct{}
	for i, m := range msgs {
		if m.Role == "tool" {
			callID := extractToolCallID(m)
			if callID == "" {
				removeSet[i] = struct{}{}
				continue
			}
			if _, ok := activeCallIDs[callID]; !ok {
				removeSet[i] = struct{}{}
			}
			continue
		}
		activeCallIDs = nil
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			activeCallIDs = make(map[string]struct{}, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				activeCallIDs[tc.ToolCallID] = struct{}{}
			}
		}
	}

	// pass 2: 收集所有存活的 tool_call_id
	survivingToolCallIDs := make(map[string]struct{})
	for i, m := range msgs {
		if _, removed := removeSet[i]; removed {
			continue
		}
		if m.Role == "tool" {
			if callID := extractToolCallID(m); callID != "" {
				survivingToolCallIDs[callID] = struct{}{}
			}
		}
	}

	// pass 3: 仅保留 assistant(tool_use) 中仍有对应 tool result 的 ToolCalls。
	// 如果一条 assistant 最终没有可保留的 ToolCalls，且也没有可见文本，则整条移除。
	for i, m := range msgs {
		if _, removed := removeSet[i]; removed {
			continue
		}
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		kept := make([]llm.ToolCall, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if _, ok := survivingToolCallIDs[tc.ToolCallID]; ok {
				kept = append(kept, tc)
			}
		}
		if len(kept) == len(m.ToolCalls) {
			continue
		}
		if len(kept) == 0 {
			if i+1 < len(msgs) && msgs[i+1].Role == "tool" {
				removeSet[i] = struct{}{}
				continue
			}
			prunedToolCalls[i] = nil
			continue
		}
		prunedToolCalls[i] = kept
	}

	if len(removeSet) == 0 && len(prunedToolCalls) == 0 {
		return msgs, ids
	}

	alignedIDs := len(ids) == len(msgs)
	outMsgs := make([]llm.Message, 0, len(msgs)-len(removeSet))
	var outIDs []uuid.UUID
	if alignedIDs {
		outIDs = make([]uuid.UUID, 0, len(msgs)-len(removeSet))
	}
	for i, m := range msgs {
		if _, skip := removeSet[i]; skip {
			continue
		}
		if kept, ok := prunedToolCalls[i]; ok {
			m.ToolCalls = kept
		}
		outMsgs = append(outMsgs, m)
		if alignedIDs {
			outIDs = append(outIDs, ids[i])
		}
	}
	return outMsgs, outIDs
}

func extractToolCallID(m llm.Message) string {
	if len(m.Content) == 0 {
		return ""
	}
	var envelope struct {
		ToolCallID string `json:"tool_call_id"`
	}
	if json.Unmarshal([]byte(m.Content[0].Text), &envelope) != nil {
		return ""
	}
	return envelope.ToolCallID
}

func budgetOK(msgs []llm.Message, start int, cfg ContextCompactSettings, enc *tiktoken.Tiktoken) bool {
	if cfg.MaxMessages > 0 && len(msgs)-start > cfg.MaxMessages {
		return false
	}
	if cfg.MaxUserMessageTokens > 0 && SuffixRoleAndContentTokens(enc, msgs, start, true) > cfg.MaxUserMessageTokens {
		return false
	}
	if cfg.MaxTotalTextTokens > 0 && SuffixRoleAndContentTokens(enc, msgs, start, false) > cfg.MaxTotalTextTokens {
		return false
	}
	if cfg.MaxUserTextBytes > 0 && countUserBytes(msgs, start) > cfg.MaxUserTextBytes {
		return false
	}
	if cfg.MaxTotalTextBytes > 0 && countTotalBytes(msgs, start) > cfg.MaxTotalTextBytes {
		return false
	}
	return true
}

// CompactThreadMessages 的 delete-based trim 语义已退役。
// 历史 compact 只允许通过 replacement frontier 表达，因此这里保持 no-op。
func CompactThreadMessages(msgs []llm.Message, ids []uuid.UUID, cfg ContextCompactSettings, enc *tiktoken.Tiktoken) ([]llm.Message, []uuid.UUID, int) {
	return msgs, alignIDs(ids, len(msgs)), 0
}

func alignIDs(ids []uuid.UUID, n int) []uuid.UUID {
	if len(ids) == n {
		return ids
	}
	return nil
}

const (
	tailTruncateThresholdTokens = 2000
	tailTruncatePreviewTokens   = 512
	tailTruncatePreviewRunes    = 1600
)

// truncateLargeTailMessages 对尾部保留区里超过阈值的 user 消息截断为预览（内存副本，不改 DB）。
// 最后一条 user 消息不截断（用户正在讨论的内容）。
func truncateLargeTailMessages(enc *tiktoken.Tiktoken, msgs []llm.Message) []llm.Message {
	if enc == nil || len(msgs) == 0 {
		return msgs
	}
	lastUserIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if i == lastUserIdx || out[i].Role != "user" {
			continue
		}
		text := messageText(out[i])
		if shouldUseCompactRuneFallback(text) {
			estimated := approxTokensFromText(text)
			if estimated <= tailTruncateThresholdTokens {
				continue
			}
			preview := compactRunePreview(text, tailTruncatePreviewRunes)
			truncated := fmt.Sprintf("%s\n\n[... content truncated (%d tokens approx) ...]", preview, estimated)
			out[i] = llm.Message{
				Role:    out[i].Role,
				Phase:   out[i].Phase,
				Content: []llm.ContentPart{{Type: "text", Text: truncated}},
			}
			continue
		}
		encoded := enc.Encode(text, nil, nil)
		if len(encoded) <= tailTruncateThresholdTokens {
			continue
		}
		preview := enc.Decode(encoded[:tailTruncatePreviewTokens])
		truncated := fmt.Sprintf("%s\n\n[... content truncated (%d tokens) ...]", preview, len(encoded))
		out[i] = llm.Message{
			Role:    out[i].Role,
			Phase:   out[i].Phase,
			Content: []llm.ContentPart{{Type: "text", Text: truncated}},
		}
	}
	return out
}

// computeTailKeepByTokenBudget 从 msgs 末尾往前累加 token，在 tokenBudget 和 maxMessages 双重约束下返回保留条数。
func computeTailKeepByTokenBudget(enc *tiktoken.Tiktoken, msgs []llm.Message, tokenBudget int, maxMessages int) int {
	if len(msgs) == 0 || tokenBudget <= 0 {
		return 0
	}
	const tokensPerMessage = 3
	accum := 0
	keep := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		mt := tokensPerMessage + len(enc.Encode(msgs[i].Role, nil, nil)) + len(enc.Encode(messageText(msgs[i]), nil, nil))
		mt += contextCompactImageTokens(msgs[i])
		if keep > 0 && accum+mt > tokenBudget {
			break
		}
		accum += mt
		keep++
		if maxMessages > 0 && keep >= maxMessages {
			break
		}
	}
	return keep
}

// ContextCompactHasActiveBudget enabled 为真且至少有一项预算大于 0。
func ContextCompactHasActiveBudget(cfg ContextCompactSettings) bool {
	if !cfg.Enabled {
		return false
	}
	return cfg.MaxMessages > 0 ||
		cfg.MaxUserMessageTokens > 0 ||
		cfg.MaxTotalTextTokens > 0 ||
		cfg.MaxUserTextBytes > 0 ||
		cfg.MaxTotalTextBytes > 0
}

func RewriteOversizeRequest(
	ctx context.Context,
	rc *RunContext,
	request llm.Request,
	anchor *ContextCompactPressureAnchor,
	requestEstimate func(llm.Request) (int, error),
) (llm.Request, OversizeRewriteStats, error) {
	stats := OversizeRewriteStats{}
	if rc == nil {
		return request, stats, fmt.Errorf("provider request estimator unavailable")
	}
	if requestEstimate == nil {
		return request, stats, fmt.Errorf("provider request estimator unavailable")
	}
	currentEstimate, err := requestEstimate(request)
	if err != nil {
		return request, stats, err
	}
	contextWindowTokens := ResolveRunContextWindowTokens(rc)
	stats.RequestBytesBeforeRewrite = currentEstimate
	stats.ContextWindowTokens = contextWindowTokens
	stats.RequestTokensBeforeRewrite = ComputeContextCompactPressure(EstimateRequestContextTokens(rc, request), anchor).ContextPressureTokens
	minimalRequest := minimalCurrentInputRequest(request)
	stats.MinimalRequestBytes, err = requestEstimate(minimalRequest)
	if err != nil {
		return request, stats, err
	}
	stats.MinimalRequestTokens = ComputeContextCompactPressure(EstimateRequestContextTokens(rc, minimalRequest), anchor).ContextPressureTokens
	if llm.RequestExceedsLimits(stats.MinimalRequestBytes, stats.MinimalRequestTokens, contextWindowTokens) {
		stats.CurrentInputTooLarge = true
		stats.RequestBytesAfterRewrite = stats.RequestBytesBeforeRewrite
		stats.RequestTokensAfterRewrite = stats.RequestTokensBeforeRewrite
		return request, stats, &CurrentInputOversizeError{
			CurrentRequestEstimate: stats.RequestBytesBeforeRewrite,
			MinimalRequestEstimate: stats.MinimalRequestBytes,
			CurrentRequestTokens:   stats.RequestTokensBeforeRewrite,
			MinimalRequestTokens:   stats.MinimalRequestTokens,
			ContextWindowTokens:    contextWindowTokens,
		}
	}

	rewritten := request
	var stripped int
	rewritten.Messages, stripped = stripOlderImagePartsKeepingTail(rewritten.Messages, resolveContextKeepImageTail())
	if stripped > 0 {
		stats.RewriteApplied = true
		stats.ImagesStripped = stripped
	}

	var microcompacted int
	rewritten.Messages, microcompacted = microcompactToolResultsWithCount(rewritten.Messages, rc.ContextCompact.MicrocompactKeepRecentTools)
	if microcompacted > 0 {
		stats.RewriteApplied = true
		stats.ToolResultsMicrocompacted = microcompacted
	}

	stats.RequestBytesAfterRewrite, err = requestEstimate(rewritten)
	if err != nil {
		return request, stats, err
	}
	stats.RequestTokensAfterRewrite = ComputeContextCompactPressure(EstimateRequestContextTokens(rc, rewritten), anchor).ContextPressureTokens
	if !llm.RequestExceedsLimits(
		stats.RequestBytesAfterRewrite,
		stats.RequestTokensAfterRewrite,
		contextWindowTokens,
	) {
		return rewritten, stats, nil
	}

	emergencyCompact, compactStats, changed, compactErr := rewriteOversizeRequestWithPersistedReplacement(ctx, rc, rewritten, anchor, requestEstimate)
	if compactErr != nil {
		return request, stats, compactErr
	}
	if changed {
		rewritten = emergencyCompact
		stats.RewriteApplied = true
		stats.CompactApplied = true
		stats.TargetChunkCount = compactStats.TargetChunkCount
		stats.PreviousReplacementCount = compactStats.PreviousReplacementCount
		stats.SingleAtomPartial = compactStats.SingleAtomPartial
		stats.RequestBytesAfterRewrite = compactStats.ContextEstimateTokens
	}

	stats.RequestBytesAfterRewrite, err = requestEstimate(rewritten)
	if err != nil {
		return request, stats, err
	}
	stats.RequestTokensAfterRewrite = ComputeContextCompactPressure(EstimateRequestContextTokens(rc, rewritten), anchor).ContextPressureTokens
	return rewritten, stats, nil
}

func rewriteOversizeRequestWithPersistedReplacement(
	ctx context.Context,
	rc *RunContext,
	request llm.Request,
	anchor *ContextCompactPressureAnchor,
	requestEstimate func(llm.Request) (int, error),
) (llm.Request, ContextCompactPressureStats, bool, error) {
	stats := ComputeContextCompactPressure(EstimateRequestContextTokens(rc, request), anchor)
	if rc == nil || rc.DB == nil || rc.Gateway == nil || rc.SelectedRoute == nil {
		return request, stats, false, nil
	}
	window := ResolveRunContextWindowTokens(rc)
	if window <= 0 {
		return request, stats, false, nil
	}
	current := request
	changedAny := false
	for round := 1; ; round++ {
		currentBytes, err := requestEstimate(current)
		if err != nil {
			return request, stats, changedAny, err
		}
		stats = ComputeContextCompactPressure(EstimateRequestContextTokens(rc, current), anchor)
		if !llm.RequestExceedsLimits(currentBytes, stats.ContextPressureTokens, window) {
			return current, stats, changedAny, nil
		}
		next, roundStats, changed, err := persistEmergencyReplacementRound(ctx, rc, current, anchor, round)
		if err != nil {
			return request, stats, changedAny, err
		}
		if !changed {
			return current, stats, changedAny, nil
		}
		current = next
		stats = roundStats
		changedAny = true
	}
}

func persistEmergencyReplacementRound(
	ctx context.Context,
	rc *RunContext,
	request llm.Request,
	anchor *ContextCompactPressureAnchor,
	round int,
) (llm.Request, ContextCompactPressureStats, bool, error) {
	stats := ComputeContextCompactPressure(EstimateRequestContextTokens(rc, request), anchor)
	if rc == nil || rc.DB == nil || rc.Gateway == nil || rc.SelectedRoute == nil {
		return request, stats, false, nil
	}
	releaseLock, err := CompactThreadCompactionLock(ctx, rc.Run.ThreadID)
	if err != nil {
		return request, stats, false, err
	}
	defer releaseLock()
	basePrefixCount := compactRequestBasePrefixCount(request.Messages, rc.Messages)
	if basePrefixCount <= 0 || len(rc.ThreadContextFrontier) == 0 {
		return request, stats, false, nil
	}
	tx, err := rc.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return request, stats, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := compactThreadCompactionAdvisoryXactLock(ctx, tx, rc.Run.ThreadID); err != nil {
		return request, stats, false, err
	}
	canonical, err := buildCanonicalThreadContext(ctx, tx, rc.Run, data.MessagesRepository{}, nil, nil, 0)
	if err != nil {
		return request, stats, false, err
	}
	if len(canonical.Frontier) == 0 {
		return request, stats, false, nil
	}
	selection, ok := selectPersistFrontierWindowForPressure(rc, canonical.Frontier, stats.ContextPressureTokens)
	if !ok {
		return request, stats, false, nil
	}
	if hostMode == "desktop" {
		if err := tx.Commit(ctx); err != nil {
			return request, stats, false, err
		}
		committed = true
		tx = nil
	}
	progress := newCompactProgressRecorder(rc.DB, data.RunEventsRepository{}, map[string]any{
		"op":    "persist_emergency",
		"round": round,
	})
	progress.emit(ctx, rc, "round_started", map[string]any{
		"context_pressure_tokens": stats.ContextPressureTokens,
	})
	summary, usedNodes, err := compactNodesWithPersistRetry(ctx, rc, rc.Gateway, rc.SelectedRoute.Route.Model, selection, progress)
	if err != nil {
		progress.emit(ctx, rc, "llm_failed", map[string]any{"error": err.Error()})
		return request, stats, false, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" || len(usedNodes) == 0 {
		return request, stats, false, nil
	}
	persistNodes := mapSelectedAtomsToPersistFrontierNodes(usedNodes, canonical.Frontier)
	if len(persistNodes) == 0 {
		persistNodes = append([]FrontierNode(nil), usedNodes...)
	}
	if hostMode == "desktop" {
		tx, err = rc.DB.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return request, stats, false, err
		}
		committed = false
	}
	plan, ok, err := resolvePersistReplacementPlan(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, persistNodes)
	if err != nil {
		return request, stats, false, err
	}
	if !ok {
		return request, stats, false, nil
	}
	replacementsRepo := data.ThreadContextReplacementsRepository{}
	replacement, err := replacementsRepo.Insert(ctx, tx, data.ThreadContextReplacementInsertInput{
		AccountID:       rc.Run.AccountID,
		ThreadID:        rc.Run.ThreadID,
		StartThreadSeq:  plan.StartThreadSeq,
		EndThreadSeq:    plan.EndThreadSeq,
		StartContextSeq: plan.StartContextSeq,
		EndContextSeq:   plan.EndContextSeq,
		SummaryText:     summary,
		Layer:           plan.Layer,
		MetadataJSON:    compactReplacementMetadata("context_compact_emergency"),
	})
	if err != nil {
		return request, stats, false, err
	}
	if err := writeReplacementSupersessionEdges(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, replacement.ID, plan); err != nil {
		return request, stats, false, err
	}
	if err := replacementsRepo.SupersedeActiveOverlapsByContextSeq(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, replacement.StartContextSeq, replacement.EndContextSeq, replacement.ID); err != nil {
		return request, stats, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return request, stats, false, err
	}
	committed = true

	rebuilt, err := rebuildCanonicalThreadContextForCompact(ctx, rc)
	if err != nil {
		return request, stats, false, err
	}
	rc.Messages = append([]llm.Message(nil), rebuilt.Messages...)
	rc.ThreadMessageIDs = append([]uuid.UUID(nil), rebuilt.ThreadMessageIDs...)
	rc.ThreadContextFrontier = append([]FrontierNode(nil), rebuilt.Frontier...)
	rebuiltRequest := request
	rebuiltRequest.Messages = applyRebuiltHistoryToRequest(rebuilt.Messages, request.Messages, basePrefixCount)
	rebuiltStats := ComputeContextCompactPressure(EstimateRequestContextTokens(rc, rebuiltRequest), anchor)
	rebuiltStats.TargetChunkCount = len(usedNodes)
	rebuiltStats.PreviousReplacementCount = countReplacementFrontierNodes(canonical.Frontier)
	progress.emit(ctx, rc, "round_completed", map[string]any{
		"context_pressure_tokens": rebuiltStats.ContextPressureTokens,
		"target_chunk_count":      rebuiltStats.TargetChunkCount,
	})
	return rebuiltRequest, rebuiltStats, true, nil
}

func rebuildCanonicalThreadContextForCompact(ctx context.Context, rc *RunContext) (*canonicalThreadContext, error) {
	return rebuildCanonicalThreadContextForCompactUpTo(ctx, rc, nil)
}

func rebuildCanonicalThreadContextForCompactUpTo(ctx context.Context, rc *RunContext, upperBoundMessageID *uuid.UUID) (*canonicalThreadContext, error) {
	if rc == nil || rc.DB == nil {
		return nil, fmt.Errorf("run context db unavailable")
	}
	tx, err := rc.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return buildCanonicalThreadContext(ctx, tx, rc.Run, data.MessagesRepository{}, nil, upperBoundMessageID, 0)
}

func selectEmergencyPersistFrontierWindow(
	rc *RunContext,
	frontier []FrontierNode,
	pressureTokens int,
) ([]FrontierNode, ContextCompactPressureStats, bool) {
	selection, ok := selectPersistFrontierWindowForPressure(rc, frontier, pressureTokens)
	stats := ContextCompactPressureStats{ContextPressureTokens: pressureTokens, ContextEstimateTokens: pressureTokens}
	if !ok {
		return nil, stats, false
	}
	return selection, stats, true
}

func selectPersistFrontierWindowForPressure(
	rc *RunContext,
	frontier []FrontierNode,
	pressureTokens int,
) ([]FrontierNode, bool) {
	selectionFrontier := buildCompactFrontierAtomsFromPersistFrontier(frontier)
	if len(selectionFrontier) == 0 {
		return nil, false
	}
	window := ResolveRunContextWindowTokens(rc)
	targetTokens := contextCompactTargetTokens(rc.ContextCompact, window)
	if targetTokens <= 0 {
		targetTokens = window * 65 / 100
	}
	if targetTokens <= 0 {
		targetTokens = 1
	}
	rawBudget := window - targetTokens
	if pressureTokens <= targetTokens {
		rawBudget = 0
	}
	rawStart := recentRawZoneStartIndex(selectionFrontier, rawBudget)
	if rawStart <= 0 {
		return nil, false
	}
	eligible := selectionFrontier[:rawStart]

	// 双分支选区：根据 compact zone 是否满来决定选谁
	compactZoneBudgetPct := rc.ContextCompact.CompactZoneBudgetPct
	if compactZoneBudgetPct <= 0 {
		compactZoneBudgetPct = 25
	}
	compactZoneBudget := window * compactZoneBudgetPct / 100
	compactZoneTokens := 0
	for _, node := range eligible {
		if node.Kind == FrontierNodeReplacement {
			compactZoneTokens += node.ApproxTokens
		}
	}
	if compactZoneTokens >= compactZoneBudget {
		// 10.2: compact zone 满了，只选 replacement 做 compact of compact
		replacementsOnly := make([]FrontierNode, 0, len(eligible))
		for _, node := range eligible {
			if node.Kind == FrontierNodeReplacement {
				replacementsOnly = append(replacementsOnly, node)
			}
		}
		// 11.1: 禁止单独 compact 一个 replacement，至少要 2 个
		if len(replacementsOnly) > 1 {
			eligible = replacementsOnly
		}
	} else {
		// 10.1: compact zone 没满，只选 raw chunk，让覆盖范围向右推进
		rawOnly := make([]FrontierNode, 0, len(eligible))
		for _, node := range eligible {
			if node.Kind != FrontierNodeReplacement {
				rawOnly = append(rawOnly, node)
			}
		}
		if len(rawOnly) > 0 {
			eligible = rawOnly
		}
	}

	selection := selectCompactAtomWindow(eligible, pressureTokens-targetTokens, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) == 0 {
		return nil, false
	}
	return selection.Nodes, true
}

func recentRawZoneStartIndex(nodes []FrontierNode, rawBudget int) int {
	if len(nodes) == 0 {
		return 0
	}
	lastAtomSeq := nodes[len(nodes)-1].AtomSeq
	start := len(nodes) - 1
	for start > 0 && nodes[start-1].AtomSeq == lastAtomSeq {
		start--
	}
	accum := compactNodesApproxTokens(nodes[start:])
	if rawBudget < accum {
		rawBudget = accum
	}
	for i := start - 1; i >= 0; {
		atomSeq := nodes[i].AtomSeq
		atomStart := i
		atomTokens := 0
		for atomStart >= 0 && nodes[atomStart].AtomSeq == atomSeq {
			atomTokens += nodes[atomStart].ApproxTokens
			atomStart--
		}
		if accum+atomTokens > rawBudget {
			break
		}
		accum += atomTokens
		start = atomStart + 1
		i = atomStart
	}
	return start
}

func compactRequestBasePrefixCount(requestMessages []llm.Message, baseMessages []llm.Message) int {
	if len(requestMessages) == 0 || len(baseMessages) == 0 {
		return 0
	}
	if len(baseMessages) > len(requestMessages) {
		return len(requestMessages)
	}
	for i := 0; i < len(baseMessages); i++ {
		if !compactMessagesEquivalentForPrefix(baseMessages[i], requestMessages[i]) {
			return len(baseMessages)
		}
	}
	return len(baseMessages)
}

func compactMessagesEquivalentForPrefix(left llm.Message, right llm.Message) bool {
	if strings.TrimSpace(left.Role) != strings.TrimSpace(right.Role) {
		return false
	}
	leftPhase := ""
	if left.Phase != nil {
		leftPhase = strings.TrimSpace(*left.Phase)
	}
	rightPhase := ""
	if right.Phase != nil {
		rightPhase = strings.TrimSpace(*right.Phase)
	}
	if leftPhase != rightPhase {
		return false
	}
	return strings.TrimSpace(messageText(left)) == strings.TrimSpace(messageText(right))
}

func applyRebuiltHistoryToRequest(rebuiltBase []llm.Message, current []llm.Message, basePrefixCount int) []llm.Message {
	tailCount := 0
	if basePrefixCount < len(current) {
		tailCount = len(current) - basePrefixCount
	}
	out := make([]llm.Message, 0, len(rebuiltBase)+tailCount)
	out = append(out, cloneLLMMessages(rebuiltBase)...)
	if basePrefixCount < len(current) {
		out = append(out, cloneLLMMessages(current[basePrefixCount:])...)
	}
	return out
}

func cloneLLMMessages(src []llm.Message) []llm.Message {
	if len(src) == 0 {
		return nil
	}
	out := make([]llm.Message, len(src))
	copy(out, src)
	return out
}

func countReplacementFrontierNodes(frontier []FrontierNode) int {
	total := 0
	for _, node := range frontier {
		if node.Kind == FrontierNodeReplacement {
			total++
		}
	}
	return total
}

func ExecuteContextCompactMaintenanceJob(
	ctx context.Context,
	rc *RunContext,
	upperBoundMessageID *uuid.UUID,
	eventsRepo CompactRunEventAppender,
) error {
	if rc == nil || rc.DB == nil || rc.Gateway == nil || rc.SelectedRoute == nil {
		return nil
	}
	releaseLock, err := CompactThreadCompactionLock(ctx, rc.Run.ThreadID)
	if err != nil {
		return err
	}
	defer releaseLock()
	if failures := compactConsecutiveFailures(ctx, rc.DB, rc.Run.AccountID, rc.Run.ThreadID); failures >= maxConsecutiveCompactFailures {
		appendErr := appendContextCompactRunEvent(ctx, rc.DB, eventsRepo, rc, map[string]any{
			"op":    "persist_background",
			"phase": "circuit_breaker",
		})
		if appendErr != nil {
			return appendErr
		}
		return nil
	}

	var anchorPtr *ContextCompactPressureAnchor
	if anchor, ok := resolveContextCompactPressureAnchor(ctx, rc.DB, rc); ok {
		anchorCopy := anchor
		anchorPtr = &anchorCopy
	}

	progress := newCompactProgressRecorder(rc.DB, eventsRepo, map[string]any{
		"op": "persist_background",
	})
	progress.emit(ctx, rc, "evaluating", nil)

	window := ResolveRunContextWindowTokens(rc)
	targetTokens := contextCompactTargetTokens(rc.ContextCompact, window)
	if targetTokens <= 0 {
		targetTokens = window * 65 / 100
	}
	if targetTokens <= 0 {
		targetTokens = 1
	}

	const maxCompactRounds = 10
	for round := 1; ; round++ {
		if round > maxCompactRounds {
			progress.emit(ctx, rc, "max_rounds_reached", map[string]any{
				"max_rounds": maxCompactRounds,
			})
			return nil
		}
		tx, err := rc.DB.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		if err := compactThreadCompactionAdvisoryXactLock(ctx, tx, rc.Run.ThreadID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		canonical, err := buildCanonicalThreadContext(ctx, tx, rc.Run, data.MessagesRepository{}, nil, upperBoundMessageID, 0)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		stats := ComputeContextCompactPressure(EstimateRequestContextTokens(rc, llm.Request{Messages: canonical.Messages}), anchorPtr)
		if stats.ContextPressureTokens <= targetTokens {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			progress.emit(ctx, rc, "completed", map[string]any{
				"context_pressure_tokens": stats.ContextPressureTokens,
			})
			return nil
		}
		selection, ok := selectPersistFrontierWindowForPressure(rc, canonical.Frontier, stats.ContextPressureTokens)
		if !ok {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			progress.emit(ctx, rc, "completed", map[string]any{
				"context_pressure_tokens": stats.ContextPressureTokens,
			})
			return nil
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		progress.emit(ctx, rc, "round_started", map[string]any{
			"round":                   round,
			"context_pressure_tokens": stats.ContextPressureTokens,
		})
		summary, usedNodes, err := compactNodesWithPersistRetry(ctx, rc, rc.Gateway, rc.SelectedRoute.Route.Model, selection, progress)
		if err != nil {
			progress.emit(ctx, rc, "llm_failed", map[string]any{
				"round": round,
				"error": err.Error(),
			})
			return err
		}
		summary = strings.TrimSpace(summary)
		if summary == "" || len(usedNodes) == 0 {
			progress.emit(ctx, rc, "completed", map[string]any{
				"round": round,
			})
			return nil
		}
		persistNodes := mapSelectedAtomsToPersistFrontierNodes(usedNodes, canonical.Frontier)
		if len(persistNodes) == 0 {
			persistNodes = append([]FrontierNode(nil), usedNodes...)
		}
		tx, err = rc.DB.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		plan, ok, err := resolvePersistReplacementPlan(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, persistNodes)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if !ok {
			_ = tx.Rollback(ctx)
			return nil
		}
		replacementsRepo := data.ThreadContextReplacementsRepository{}
		replacement, err := replacementsRepo.Insert(ctx, tx, data.ThreadContextReplacementInsertInput{
			AccountID:       rc.Run.AccountID,
			ThreadID:        rc.Run.ThreadID,
			StartThreadSeq:  plan.StartThreadSeq,
			EndThreadSeq:    plan.EndThreadSeq,
			StartContextSeq: plan.StartContextSeq,
			EndContextSeq:   plan.EndContextSeq,
			SummaryText:     summary,
			Layer:           plan.Layer,
			MetadataJSON:    compactReplacementMetadata("context_compact_background"),
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := writeReplacementSupersessionEdges(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, replacement.ID, plan); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := replacementsRepo.SupersedeActiveOverlapsByContextSeq(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, replacement.StartContextSeq, replacement.EndContextSeq, replacement.ID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}

		rebuilt, err := rebuildCanonicalThreadContextForCompactUpTo(ctx, rc, upperBoundMessageID)
		if err != nil {
			return err
		}
		rc.Messages = append([]llm.Message(nil), rebuilt.Messages...)
		rc.ThreadMessageIDs = append([]uuid.UUID(nil), rebuilt.ThreadMessageIDs...)
		rc.ThreadContextFrontier = append([]FrontierNode(nil), rebuilt.Frontier...)
		rebuiltStats := ComputeContextCompactPressure(EstimateRequestContextTokens(rc, llm.Request{Messages: rebuilt.Messages}), anchorPtr)
		progress.emit(ctx, rc, "round_completed", map[string]any{
			"round":                   round,
			"context_pressure_tokens": rebuiltStats.ContextPressureTokens,
			"target_chunk_count":      len(usedNodes),
		})
		if rebuiltStats.ContextPressureTokens <= targetTokens {
			progress.emit(ctx, rc, "completed", map[string]any{
				"round":                   round,
				"context_pressure_tokens": rebuiltStats.ContextPressureTokens,
			})
			return nil
		}
	}
}

func ResolveRunContextWindowTokens(rc *RunContext) int {
	if rc == nil {
		return defaultFallbackContextWindowTokens
	}
	if rc.ContextWindowTokens > 0 {
		return rc.ContextWindowTokens
	}
	if rc.ContextCompact.FallbackContextWindowTokens > 0 {
		return rc.ContextCompact.FallbackContextWindowTokens
	}
	return defaultFallbackContextWindowTokens
}

func compactApproxMessagePressure(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += approxTokensFromText(messageText(m))
		for _, tc := range m.ToolCalls {
			total += approxTokensFromText(tc.ToolName)
			if len(tc.ArgumentsJSON) > 0 {
				if raw, err := json.Marshal(tc.ArgumentsJSON); err == nil {
					total += approxTokensFromText(string(raw))
				}
			}
		}
	}
	return total
}

func EstimateRequestContextTokens(rc *RunContext, request llm.Request) int {
	// system prompt 统一从 rc.SystemPrompt 估算，跳过 messages 里的 system 角色消息避免重复
	estimate := compactApproxNonSystemMessagePressure(request.Messages)
	if rc != nil && rc.SystemPrompt != "" {
		estimate += approxTokensFromText(rc.SystemPrompt)
	}
	// PromptPlan 的 MessageBlocks（非 system 部分）
	if request.PromptPlan != nil {
		for _, block := range request.PromptPlan.MessageBlocks {
			estimate += approxTokensFromText(strings.TrimSpace(block.Text))
		}
	}
	// tool schemas 的 token 开销
	for _, t := range request.Tools {
		estimate += approxTokensFromText(t.Name)
		if t.Description != nil {
			estimate += approxTokensFromText(*t.Description)
		}
		if len(t.JSONSchema) > 0 {
			if raw, err := json.Marshal(t.JSONSchema); err == nil {
				estimate += approxTokensFromText(string(raw))
			}
		}
	}
	if estimate < 1 {
		return 1
	}
	return estimate
}

// compactApproxNonSystemMessagePressure 与 compactApproxMessagePressure 相同，但跳过 role="system"
func compactApproxNonSystemMessagePressure(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}
		total += approxTokensFromText(messageText(m))
		for _, tc := range m.ToolCalls {
			total += approxTokensFromText(tc.ToolName)
			if len(tc.ArgumentsJSON) > 0 {
				if raw, err := json.Marshal(tc.ArgumentsJSON); err == nil {
					total += approxTokensFromText(string(raw))
				}
			}
		}
	}
	return total
}

func EstimateProviderRequestBytesForRunContext(rc *RunContext, request llm.Request) (int, error) {
	if rc == nil || rc.SelectedRoute == nil {
		return 0, fmt.Errorf("provider request estimator unavailable")
	}
	resolved, err := ResolveGatewayConfigFromSelectedRoute(*rc.SelectedRoute, false, rc.LlmMaxResponseBytes)
	if err != nil {
		return 0, err
	}
	return llm.EstimateProviderPayloadBytes(resolved, request)
}

func minimalCurrentInputRequest(request llm.Request) llm.Request {
	minimal := request
	minimal.Messages = minimalCurrentInputMessages(request.Messages)
	minimal.PromptPlan = minimalPromptPlanForCurrentInput(request.PromptPlan)
	return minimal
}

func minimalCurrentInputMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	lastIdx := len(messages) - 1
	out := make([]llm.Message, 0, len(messages))
	for idx, msg := range messages {
		if msg.Role == "system" {
			out = append(out, msg)
			continue
		}
		if idx == lastIdx {
			out = append(out, msg)
		}
	}
	if len(out) == 0 {
		out = append(out, messages[lastIdx])
	}
	return out
}

func minimalPromptPlanForCurrentInput(plan *llm.PromptPlan) *llm.PromptPlan {
	if plan == nil {
		return nil
	}
	minimal := &llm.PromptPlan{
		SystemBlocks: append([]llm.PromptPlanBlock(nil), plan.SystemBlocks...),
	}
	return minimal
}

func promptPlanTextTokens(plan *llm.PromptPlan) int {
	if plan == nil {
		return 0
	}
	total := 0
	for _, block := range plan.SystemBlocks {
		total += approxTokensFromText(strings.TrimSpace(block.Text))
	}
	for _, block := range plan.MessageBlocks {
		total += approxTokensFromText(strings.TrimSpace(block.Text))
	}
	return total
}

// microcompactToolResults 保留最近 keepRecent 个 role="tool" 消息原文，其余替换为占位符。
// 返回新 slice，不修改原始数据。
func microcompactToolResults(msgs []llm.Message, keepRecent int) []llm.Message {
	out, _ := microcompactToolResultsWithCount(msgs, keepRecent)
	return out
}

func microcompactToolResultsWithCount(msgs []llm.Message, keepRecent int) ([]llm.Message, int) {
	if keepRecent <= 0 || len(msgs) == 0 {
		return msgs, 0
	}

	// 从末尾往前收集 tool 消息索引
	toolIndices := make([]int, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "tool" {
			toolIndices = append(toolIndices, i)
		}
	}
	if len(toolIndices) <= keepRecent {
		return msgs, 0
	}

	// toolIndices[0..keepRecent-1] 是从末尾数的最近 N 个，保留；其余需清理
	clearSet := make(map[int]struct{}, len(toolIndices)-keepRecent)
	for _, idx := range toolIndices[keepRecent:] {
		clearSet[idx] = struct{}{}
	}

	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	cleared := 0
	for idx := range clearSet {
		out[idx] = microcompactedStub(out[idx])
		cleared++
	}
	return out, cleared
}

// microcompactedStub replaces a tool message's result with a minimal stub
// while preserving tool_call_id and tool_name so downstream LLM providers
// can maintain conversation structure.
func microcompactedStub(m llm.Message) llm.Message {
	stub := map[string]any{"result": map[string]any{"cleared": true}}
	if len(m.Content) > 0 {
		var envelope map[string]any
		if json.Unmarshal([]byte(m.Content[0].Text), &envelope) == nil {
			if id, ok := envelope["tool_call_id"]; ok {
				stub["tool_call_id"] = id
			}
			if name, _ := envelope["tool_name"].(string); strings.TrimSpace(name) != "" {
				stub["tool_name"] = strings.TrimSpace(name)
			}
		}
	}
	text, _ := json.Marshal(stub)
	var trustSource string
	if len(m.Content) > 0 {
		trustSource = m.Content[0].TrustSource
	}
	return llm.Message{
		Role:      m.Role,
		Phase:     m.Phase,
		ToolCalls: m.ToolCalls,
		Content:   []llm.ContentPart{{Type: "text", Text: string(text), TrustSource: trustSource}},
	}
}

func stripOlderImagePartsKeepingTail(msgs []llm.Message, keepImages int) ([]llm.Message, int) {
	if len(msgs) == 0 || keepImages < 0 {
		return msgs, 0
	}
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	keepRemaining := keepImages
	stripped := 0
	for i := len(out) - 1; i >= 0; i-- {
		parts := append([]llm.ContentPart(nil), out[i].Content...)
		replaced := false
		for j := len(parts) - 1; j >= 0; j-- {
			if parts[j].Kind() != messagecontent.PartTypeImage {
				continue
			}
			if keepRemaining > 0 {
				keepRemaining--
				continue
			}
			parts[j] = llm.ContentPart{
				Type: messagecontent.PartTypeText,
				Text: contextCompactImagePlaceholder(parts[j]),
			}
			stripped++
			replaced = true
		}
		if replaced {
			out[i].Content = parts
		}
	}
	return out, stripped
}

func contextCompactImagePlaceholder(part llm.ContentPart) string {
	tag := "[image]"
	if part.Attachment != nil && strings.TrimSpace(part.Attachment.Key) != "" {
		tag = "[image attachment_key=" + strconv.Quote(part.Attachment.Key) + "]"
	}
	return tag
}

func resolveContextKeepImageTail() int {
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CONTEXT_KEEP_IMAGE_TAIL")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_GROUP_KEEP_IMAGE_TAIL")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return defaultGroupKeepImageTail
}
