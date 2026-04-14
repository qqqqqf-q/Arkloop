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
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
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

func approxTokensFromText(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
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

// CompactThreadMessages 从头部裁掉消息直到满足预算；保证切口不以孤立的 tool 开头（尽力左扩）。
// enc 为 nil 时 token 类预算退化为字节/4 近似（仅供测试）；生产路径应传入非 nil。
// ids 若与 msgs 等长则同步裁切；否则 ids 原样截断或置 nil。
func CompactThreadMessages(msgs []llm.Message, ids []uuid.UUID, cfg ContextCompactSettings, enc *tiktoken.Tiktoken) ([]llm.Message, []uuid.UUID, int) {
	if len(msgs) == 0 {
		return msgs, ids, 0
	}
	start := 0
	if cfg.MaxMessages > 0 && len(msgs) > cfg.MaxMessages {
		start = len(msgs) - cfg.MaxMessages
	}
	start = stabilizeCompactStart(msgs, start, cfg.MaxMessages)
	for start < len(msgs) && !budgetOK(msgs, start, cfg, enc) {
		start++
		start = stabilizeCompactStart(msgs, start, cfg.MaxMessages)
	}
	if start <= 0 {
		return msgs, alignIDs(ids, len(msgs)), 0
	}
	out := make([]llm.Message, len(msgs)-start)
	copy(out, msgs[start:])
	var outIDs []uuid.UUID
	if len(ids) == len(msgs) {
		outIDs = make([]uuid.UUID, len(ids)-start)
		copy(outIDs, ids[start:])
	}
	return out, outIDs, start
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

	forceCompact := llm.RequestPayloadTooLarge(stats.RequestBytesAfterRewrite)
	compacted, compactStats, changed, compactErr := MaybeInlineCompactMessages(ctx, rc, rewritten.Messages, anchor, forceCompact)
	if compactErr != nil {
		return request, stats, compactErr
	}
	if changed {
		rewritten.Messages = compacted
		stats.RewriteApplied = true
		stats.CompactApplied = true
		stats.TargetChunkCount = compactStats.TargetChunkCount
		stats.PreviousReplacementCount = compactStats.PreviousReplacementCount
		stats.SingleAtomPartial = compactStats.SingleAtomPartial
	}

	stats.RequestBytesAfterRewrite, err = requestEstimate(rewritten)
	if err != nil {
		return request, stats, err
	}
	stats.RequestTokensAfterRewrite = ComputeContextCompactPressure(EstimateRequestContextTokens(rc, rewritten), anchor).ContextPressureTokens
	return rewritten, stats, nil
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
				total += len(tc.ArgumentsJSON) / 4
			}
		}
	}
	return total
}

func EstimateRequestContextTokens(rc *RunContext, request llm.Request) int {
	estimate := compactApproxMessagePressure(request.Messages)
	if request.PromptPlan != nil {
		estimate += promptPlanTextTokens(request.PromptPlan)
	}
	if estimate < 1 {
		return 1
	}
	return estimate
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
