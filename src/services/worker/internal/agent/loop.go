package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/stablejson"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

const ErrorClassAgentMaxIterationsExceeded = "agent.max_iterations_exceeded"

type RunContext struct {
	RunID           uuid.UUID
	OrgID           *uuid.UUID
	UserID          *uuid.UUID
	AgentID         string
	ThreadID        *uuid.UUID
	TraceID         string
	InputJSON       map[string]any
	MaxIterations   int
	SystemPrompt    string
	MaxOutputTokens *int
	ToolTimeoutMs   *int
	ToolBudget      map[string]any
	ToolExecutor    *tools.DispatchingExecutor
	ToolSpecs       []llm.ToolSpec
	CancelSignal    func() bool

	// LLM 调用重试配置，0 值表示不重试
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	// IterHook 在每轮迭代完成（pending 工具调用已处理，准备进入下一轮）时被调用。
	// 返回 (text, true, nil) 时，将 text 作为 user message 注入 messages；nil 时不触发。
	IterHook func(ctx context.Context, iter int) (string, bool, error)

	// PreIterHook 在每轮迭代开始（LLM 调用之前）时被调用。
	PreIterHook func(ctx context.Context, iter int) error
}

type Loop struct {
	gateway      llm.Gateway
	toolExecutor *tools.DispatchingExecutor
}

func NewLoop(gateway llm.Gateway, toolExecutor *tools.DispatchingExecutor) *Loop {
	return &Loop{
		gateway:      gateway,
		toolExecutor: toolExecutor,
	}
}

func (l *Loop) Run(
	ctx context.Context,
	runCtx RunContext,
	request llm.Request,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) error {
	if runCtx.MaxIterations <= 0 {
		errPayload := llm.GatewayError{
			ErrorClass: ErrorClassAgentMaxIterationsExceeded,
			Message:    "agent loop reached max iterations",
			Details:    map[string]any{"max_iterations": runCtx.MaxIterations},
		}
		event := emitter.Emit("run.failed", errPayload.ToJSON(), nil, stringPtr(errPayload.ErrorClass))
		return yield(event)
	}

	messages := append([]llm.Message{}, request.Messages...)
	webSourceCount := 0
	seenToolResultKeys := map[string]toolResultDedupInfo{}
	completionTotals := newCompletionTotals()
	for iter := 1; iter <= runCtx.MaxIterations; iter++ {
		if cancelled(runCtx) {
			return yield(emitter.Emit("run.cancelled", map[string]any{"reason": "cancel_signal"}, nil, nil))
		}

		if runCtx.PreIterHook != nil {
			if err := runCtx.PreIterHook(ctx, iter); err != nil {
				return err
			}
		}

		turnRequest := copyRequest(request, messages)
		turn, err := l.runTurnWithRetry(ctx, runCtx, turnRequest, emitter, yield)
		if err != nil {
			return err
		}

		hasToolCalls := len(turn.ToolCalls) > 0
		for _, event := range turn.Events {
			// 当 turn 同时产生了 tool calls 时，跳过非 thinking 的 message.delta，
			// 避免 LLM echo 出的工具参数 JSON 被累积到最终消息内容中
			if hasToolCalls && event.Type == "message.delta" {
				if ch, _ := event.DataJSON["channel"].(string); ch == "" {
					continue
				}
			}
			if err := yield(event); err != nil {
				return err
			}
		}

		if turn.Terminal {
			return nil
		}
		if turn.Cancelled {
			return yield(emitter.Emit("run.cancelled", map[string]any{"reason": "cancel_signal"}, nil, nil))
		}

		if turn.AssistantText != "" || len(turn.ToolCalls) > 0 {
			assistantText := turn.AssistantText
			if runCtx.AgentID == "search" && len(turn.ToolCalls) > 0 {
				assistantText = ""
			}
			messages = append(messages, assistantMessage(assistantText, turn.ToolCalls))
		}

		for _, toolResult := range turn.ToolResults {
			messages = append(messages, toolResultMessage(toolResult))
		}

		if turn.CompletedDataJSON == nil {
			internal := llm.InternalStreamEndedError()
			event := emitter.Emit("run.failed", internal.ToJSON(), nil, stringPtr(internal.ErrorClass))
			return yield(event)
		}
		completionTotals.Add(turn.CompletedDataJSON)

		if len(turn.ToolCalls) == 0 {
			return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
		}

		pending := pendingToolCalls(turn.ToolCalls, turn.ToolResults)
		if len(pending) == 0 {
			return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
		}

		if cancelled(runCtx) {
			return yield(emitter.Emit("run.cancelled", map[string]any{"reason": "cancel_signal"}, nil, nil))
		}

		if runCtx.ToolExecutor == nil {
			return fmt.Errorf("tool executor not initialized")
		}
		executedCalls := l.executePendingToolCalls(ctx, runCtx, pending, emitter)
		for _, executed := range executedCalls {
			call := executed.Call
			result := executed.Result
			emittedToolCall := false
			for _, ev := range result.Events {
				if ev.Type == "tool.call" {
					emittedToolCall = true
				}
				if err := yield(ev); err != nil {
					return err
				}
			}
			if !emittedToolCall {
				if err := yield(emitter.Emit("tool.call", call.ToDataJSON(), stringPtr(call.ToolName), nil)); err != nil {
					return err
				}
			}

			resolvedID := resolveToolCallID(call.ToolCallID, result.Events)
			toolResult := toolResultFromExecution(resolvedID, call.ToolName, result)

			// web_search 结果注入累计的 1-based 引用 ID（web:N）
			if call.ToolName == "web_search" {
				webSourceCount = injectWebSourceIDs(toolResult.ResultJSON, webSourceCount)
			}

			dedupKey, sig, ok := toolResultDedupKey(call.ToolName, call.ArgumentsJSON, toolResult)
			if ok {
				if prev, exists := seenToolResultKeys[dedupKey]; exists && prev.Signature == sig {
					messages = append(messages, toolResultMessageDedup(toolResult, prev.ToolCallID))
				} else {
					seenToolResultKeys[dedupKey] = toolResultDedupInfo{
						ToolCallID: toolResult.ToolCallID,
						Signature:  sig,
					}
					messages = append(messages, toolResultMessage(toolResult))
				}
			} else {
				messages = append(messages, toolResultMessage(toolResult))
			}

			var errorClass *string
			if toolResult.Error != nil {
				errorClass = stringPtr(toolResult.Error.ErrorClass)
			}
			if err := yield(emitter.Emit("tool.result", toolResult.ToDataJSON(), stringPtr(toolResult.ToolName), errorClass)); err != nil {
				return err
			}
		}

		// 每轮迭代结束（工具调用已处理），给 InteractiveExecutor 注入用户消息的机会。
		if runCtx.IterHook != nil {
			injected, inject, hookErr := runCtx.IterHook(ctx, iter)
			if hookErr != nil {
				return hookErr
			}
			if inject && injected != "" {
				messages = append(messages, llm.Message{
					Role:    "user",
					Content: []llm.TextPart{{Text: injected}},
				})
			}
		}
	}

	errPayload := llm.GatewayError{
		ErrorClass: ErrorClassAgentMaxIterationsExceeded,
		Message:    "agent loop reached max iterations",
		Details:    map[string]any{"max_iterations": runCtx.MaxIterations},
	}
	return yield(emitter.Emit("run.failed", errPayload.ToJSON(), nil, stringPtr(errPayload.ErrorClass)))
}

type pendingToolExecution struct {
	Call   llm.ToolCall
	Result tools.ExecutionResult
}

func (l *Loop) executePendingToolCalls(
	ctx context.Context,
	runCtx RunContext,
	pending []llm.ToolCall,
	emitter events.Emitter,
) []pendingToolExecution {
	results := make([]pendingToolExecution, len(pending))
	var wg sync.WaitGroup
	wg.Add(len(pending))
	for idx := range pending {
		idx := idx
		call := pending[idx]
		go func() {
			defer wg.Done()
			execCtx := tools.ExecutionContext{
				RunID:     runCtx.RunID,
				TraceID:   runCtx.TraceID,
				OrgID:     runCtx.OrgID,
				ThreadID:  runCtx.ThreadID,
				UserID:    runCtx.UserID,
				AgentID:   runCtx.AgentID,
				TimeoutMs: runCtx.ToolTimeoutMs,
				Budget:    copyMap(runCtx.ToolBudget),
				Emitter:   emitter,
			}
			result := runCtx.ToolExecutor.Execute(ctx, call.ToolName, copyMap(call.ArgumentsJSON), execCtx, call.ToolCallID)
			results[idx] = pendingToolExecution{
				Call:   call,
				Result: result,
			}
		}()
	}
	wg.Wait()
	return results
}

type turnResult struct {
	Events            []events.RunEvent
	Terminal          bool
	Cancelled         bool
	ToolCalls         []llm.ToolCall
	ToolResults       []llm.StreamToolResult
	AssistantText     string
	CompletedDataJSON map[string]any
}

type completionTotals struct {
	inputTokens      int64
	hasInputTokens   bool
	outputTokens     int64
	hasOutputTokens  bool
	totalTokens      int64
	hasTotalTokens   bool
	cacheCreation    int64
	hasCacheCreation bool
	cacheRead        int64
	hasCacheRead     bool
	cachedTokens     int64
	hasCachedTokens  bool
	costMicros       int64
	hasCostMicros    bool
	currency         string
}

func newCompletionTotals() *completionTotals {
	return &completionTotals{}
}

func (t *completionTotals) Add(completed map[string]any) {
	if completed == nil {
		return
	}
	usage, _ := completed["usage"].(map[string]any)
	if usage != nil {
		if v, ok := anyToInt64(usage["input_tokens"]); ok {
			t.inputTokens += v
			t.hasInputTokens = true
		}
		if v, ok := anyToInt64(usage["output_tokens"]); ok {
			t.outputTokens += v
			t.hasOutputTokens = true
		}
		if v, ok := anyToInt64(usage["total_tokens"]); ok {
			t.totalTokens += v
			t.hasTotalTokens = true
		}
		if v, ok := anyToInt64(usage["cache_creation_input_tokens"]); ok {
			t.cacheCreation += v
			t.hasCacheCreation = true
		}
		if v, ok := anyToInt64(usage["cache_read_input_tokens"]); ok {
			t.cacheRead += v
			t.hasCacheRead = true
		}
		if v, ok := anyToInt64(usage["cached_tokens"]); ok {
			t.cachedTokens += v
			t.hasCachedTokens = true
		}
	}
	cost, _ := completed["cost"].(map[string]any)
	if cost != nil {
		if v, ok := anyToInt64(cost["amount_micros"]); ok {
			t.costMicros += v
			t.hasCostMicros = true
		}
		if currency, ok := cost["currency"].(string); ok && strings.TrimSpace(currency) != "" {
			t.currency = strings.TrimSpace(currency)
		}
	}
}

func (t *completionTotals) Apply(completed map[string]any) map[string]any {
	merged := copyMap(mapOrEmpty(completed))
	usage := map[string]any{}
	if t.hasInputTokens {
		usage["input_tokens"] = t.inputTokens
	}
	if t.hasOutputTokens {
		usage["output_tokens"] = t.outputTokens
	}
	if t.hasTotalTokens {
		usage["total_tokens"] = t.totalTokens
	}
	if t.hasCacheCreation {
		usage["cache_creation_input_tokens"] = t.cacheCreation
	}
	if t.hasCacheRead {
		usage["cache_read_input_tokens"] = t.cacheRead
	}
	if t.hasCachedTokens {
		usage["cached_tokens"] = t.cachedTokens
	}
	if len(usage) > 0 {
		merged["usage"] = usage
	}
	if t.hasCostMicros {
		cost := map[string]any{
			"amount_micros": t.costMicros,
		}
		if t.currency != "" {
			cost["currency"] = t.currency
		}
		merged["cost"] = cost
	}
	return merged
}

// runTurnWithRetry 在遇到 provider.retryable 失败时自动重试，并发出 run.llm.retry 事件。
// 重试期间不向调用方透传失败 turn 的事件（避免污染事件流）。
func (l *Loop) runTurnWithRetry(
	ctx context.Context,
	runCtx RunContext,
	turnRequest llm.Request,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) (turnResult, error) {
	maxAttempts := runCtx.LlmRetryMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	baseDelayMs := runCtx.LlmRetryBaseDelayMs
	if baseDelayMs <= 0 {
		baseDelayMs = 1000
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		turn, err := l.runSingleTurn(ctx, runCtx, turnRequest, emitter)
		if err != nil {
			return turnResult{}, err
		}

		// 非终态、或已用完重试次数，直接返回
		if !turn.Terminal || attempt >= maxAttempts || !isRetryableTurn(turn) {
			return turn, nil
		}

		last := turn.Events[len(turn.Events)-1]
		delayMs := retryBackoffMs(baseDelayMs, attempt)
		retryData := map[string]any{
			"attempt":      attempt,
			"max_attempts": maxAttempts,
			"delay_ms":     delayMs,
		}
		if last.ErrorClass != nil {
			retryData["error_class"] = *last.ErrorClass
		}

		if err := yield(emitter.Emit("run.llm.retry", retryData, nil, nil)); err != nil {
			return turnResult{}, err
		}

		select {
		case <-time.After(time.Duration(delayMs) * time.Millisecond):
		case <-ctx.Done():
			return turnResult{Cancelled: true}, nil
		}
	}

	return turnResult{}, fmt.Errorf("retry loop exited unexpectedly")
}

func isRetryableTurn(turn turnResult) bool {
	if len(turn.Events) == 0 {
		return false
	}
	last := turn.Events[len(turn.Events)-1]
	return last.Type == "run.failed" &&
		last.ErrorClass != nil &&
		*last.ErrorClass == llm.ErrorClassProviderRetryable
}

// retryBackoffMs 计算指数退避等待时长，最大 30s。
func retryBackoffMs(baseMs, attempt int) int {
	ms := baseMs * (1 << (attempt - 1))
	if ms > 30_000 {
		ms = 30_000
	}
	return ms
}

func (l *Loop) runSingleTurn(
	ctx context.Context,
	runCtx RunContext,
	request llm.Request,
	emitter events.Emitter,
) (turnResult, error) {
	eventsOut := []events.RunEvent{}
	toolCalls := []llm.ToolCall{}
	toolResults := []llm.StreamToolResult{}
	assistantChunks := []string{}
	var completed *llm.StreamRunCompleted

	cancelledEarly := false
	stopErr := fmt.Errorf("stop")

	err := l.gateway.Stream(ctx, request, func(item llm.StreamEvent) error {
		if cancelled(runCtx) {
			cancelledEarly = true
			return stopErr
		}

		switch typed := item.(type) {
		case llm.StreamSegmentStart:
			eventsOut = append(eventsOut, emitter.Emit("run.segment.start", typed.ToDataJSON(), nil, nil))
			return nil
		case llm.StreamSegmentEnd:
			eventsOut = append(eventsOut, emitter.Emit("run.segment.end", typed.ToDataJSON(), nil, nil))
			return nil
		case llm.StreamMessageDelta:
			if typed.ContentDelta == "" {
				return nil
			}
			// thinking 内容不计入对话上下文
			if typed.Channel == nil {
				assistantChunks = append(assistantChunks, typed.ContentDelta)
			}
			eventsOut = append(eventsOut, emitter.Emit("message.delta", typed.ToDataJSON(), nil, nil))
			return nil
		case llm.StreamLlmRequest:
			eventsOut = append(eventsOut, emitter.Emit("llm.request", typed.ToDataJSON(), nil, nil))
			return nil
		case llm.StreamLlmResponseChunk:
			eventsOut = append(eventsOut, emitter.Emit("llm.response.chunk", typed.ToDataJSON(), nil, nil))
			return nil
		case llm.StreamProviderFallback:
			eventsOut = append(eventsOut, emitter.Emit("run.provider_fallback", typed.ToDataJSON(), nil, nil))
			return nil
		case llm.ToolCall:
			toolCalls = append(toolCalls, typed)
			return nil
		case llm.StreamToolResult:
			toolResults = append(toolResults, typed)
			var errorClass *string
			if typed.Error != nil {
				errorClass = stringPtr(typed.Error.ErrorClass)
			}
			eventsOut = append(eventsOut, emitter.Emit("tool.result", typed.ToDataJSON(), stringPtr(typed.ToolName), errorClass))
			return nil
		case llm.StreamRunFailed:
			errorClass := stringPtr(typed.Error.ErrorClass)
			eventsOut = append(eventsOut, emitter.Emit("run.failed", typed.ToDataJSON(), nil, errorClass))
			return stopErr
		case llm.StreamRunCompleted:
			completed = &typed
			return stopErr
		default:
			return fmt.Errorf("unknown LLM gateway event type: %T", item)
		}
	})
	if err != nil && err != stopErr {
		return turnResult{}, err
	}

	if cancelledEarly {
		return turnResult{Events: eventsOut, Cancelled: true}, nil
	}

	if len(eventsOut) > 0 {
		last := eventsOut[len(eventsOut)-1]
		if last.Type == "run.failed" {
			return turnResult{Events: eventsOut, Terminal: true}, nil
		}
	}

	var completedJSON map[string]any
	if completed != nil {
		completedJSON = completed.ToDataJSON()
	}

	return turnResult{
		Events:            eventsOut,
		Terminal:          false,
		ToolCalls:         toolCalls,
		ToolResults:       toolResults,
		AssistantText:     strings.Join(assistantChunks, ""),
		CompletedDataJSON: completedJSON,
	}, nil
}

func copyRequest(request llm.Request, messages []llm.Message) llm.Request {
	return llm.Request{
		Model:           request.Model,
		Messages:        append([]llm.Message{}, messages...),
		Temperature:     request.Temperature,
		MaxOutputTokens: request.MaxOutputTokens,
		Tools:           append([]llm.ToolSpec{}, request.Tools...),
		Metadata:        copyMap(request.Metadata),
	}
}

func assistantMessage(text string, toolCalls []llm.ToolCall) llm.Message {
	parts := []llm.TextPart{}
	if strings.TrimSpace(text) != "" {
		parts = append(parts, llm.TextPart{Text: text})
	}
	return llm.Message{
		Role:      "assistant",
		Content:   parts,
		ToolCalls: append([]llm.ToolCall{}, toolCalls...),
	}
}

func toolResultMessage(result llm.StreamToolResult) llm.Message {
	envelope := map[string]any{
		"tool_call_id": result.ToolCallID,
	}
	if result.ResultJSON != nil {
		envelope["result"] = result.ResultJSON
	}
	if result.Error != nil {
		envelope["error"] = result.Error.ToJSON()
	}
	text, err := stablejson.Encode(envelope)
	if err != nil {
		encoded, _ := json.Marshal(envelope)
		text = string(encoded)
	}
	return llm.Message{
		Role:    "tool",
		Content: []llm.TextPart{{Text: text}},
	}
}

type toolResultDedupInfo struct {
	ToolCallID string
	Signature  string
}

func toolResultDedupKey(toolName string, args map[string]any, result llm.StreamToolResult) (string, string, bool) {
	if strings.TrimSpace(toolName) == "" || args == nil {
		return "", "", false
	}
	argsHash, err := stablejson.Sha256(args)
	if err != nil || strings.TrimSpace(argsHash) == "" {
		return "", "", false
	}

	normalizedResult := result.ResultJSON
	if toolName == "web_search" {
		normalizedResult = stripWebSearchResultIDs(result.ResultJSON)
	}

	sigPayload := map[string]any{
		"result": normalizedResult,
	}
	if result.Error != nil {
		sigPayload["error_class"] = result.Error.ErrorClass
		if strings.TrimSpace(result.Error.Message) != "" {
			sigPayload["error_message"] = result.Error.Message
		}
	}
	sig, sigErr := stablejson.Sha256(sigPayload)
	if sigErr != nil || strings.TrimSpace(sig) == "" {
		return "", "", false
	}
	return toolName + ":" + argsHash, sig, true
}

func stripWebSearchResultIDs(resultJSON map[string]any) map[string]any {
	if resultJSON == nil {
		return nil
	}
	out := copyMap(resultJSON)
	raw, has := resultJSON["results"]
	if !has || raw == nil {
		return out
	}

	switch typed := raw.(type) {
	case []map[string]any:
		results := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			entry := map[string]any{}
			for key, value := range item {
				if key == "id" {
					continue
				}
				entry[key] = value
			}
			results = append(results, entry)
		}
		out["results"] = results
		return out
	case []any:
		results := make([]map[string]any, 0, len(typed))
		for _, rawItem := range typed {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			entry := map[string]any{}
			for key, value := range item {
				if key == "id" {
					continue
				}
				entry[key] = value
			}
			results = append(results, entry)
		}
		out["results"] = results
		return out
	default:
		return out
	}
}

func toolResultMessageDedup(result llm.StreamToolResult, refToolCallID string) llm.Message {
	ref := strings.TrimSpace(refToolCallID)
	if ref == "" {
		return toolResultMessage(result)
	}
	dedup := map[string]any{
		"dedup":            "same_args_as_previous",
		"ref_tool_call_id": ref,
	}
	envelope := map[string]any{
		"tool_call_id": result.ToolCallID,
		"result":       dedup,
	}
	if result.Error != nil {
		envelope["error"] = dedup
	}
	text, err := stablejson.Encode(envelope)
	if err != nil {
		encoded, _ := json.Marshal(envelope)
		text = string(encoded)
	}
	return llm.Message{
		Role:    "tool",
		Content: []llm.TextPart{{Text: text}},
	}
}

func pendingToolCalls(toolCalls []llm.ToolCall, toolResults []llm.StreamToolResult) []llm.ToolCall {
	completed := map[string]struct{}{}
	for _, item := range toolResults {
		completed[item.ToolCallID] = struct{}{}
	}
	out := []llm.ToolCall{}
	for _, call := range toolCalls {
		if _, ok := completed[call.ToolCallID]; ok {
			continue
		}
		out = append(out, call)
	}
	return out
}

func resolveToolCallID(fallback string, eventsIn []events.RunEvent) string {
	for _, ev := range eventsIn {
		if ev.Type != "tool.call" {
			continue
		}
		raw, ok := ev.DataJSON["tool_call_id"].(string)
		if ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
	}
	return fallback
}

func toolResultFromExecution(toolCallID string, toolName string, result tools.ExecutionResult) llm.StreamToolResult {
	var errObj *llm.GatewayError
	if result.Error != nil {
		errObj = &llm.GatewayError{
			ErrorClass: result.Error.ErrorClass,
			Message:    result.Error.Message,
			Details:    copyMap(result.Error.Details),
		}
	}
	var resultJSON map[string]any
	if result.ResultJSON != nil {
		resultJSON = copyMap(result.ResultJSON)
	}
	return llm.StreamToolResult{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		ResultJSON: resultJSON,
		Error:      errObj,
	}
}

func cancelled(runCtx RunContext) bool {
	if runCtx.CancelSignal == nil {
		return false
	}
	return runCtx.CancelSignal()
}

func copyMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for key, item := range value {
		out[key] = item
	}
	return out
}

func anyToInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		return int64(typed), typed <= uint64(^uint64(0)>>1)
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}

func mapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
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

// injectWebSourceIDs 给 web_search 结果中的每条记录注入 1-based 的引用 ID（web:N），
// 保证跨多次 web_search 调用的 ID 全局唯一递增。返回更新后的累计计数。
func injectWebSourceIDs(resultJSON map[string]any, currentCount int) int {
	if resultJSON == nil {
		return currentCount
	}
	results, ok := resultJSON["results"].([]map[string]any)
	if !ok {
		// 兼容 []any 类型（JSON 反序列化后的常见类型）
		raw, ok := resultJSON["results"].([]any)
		if !ok {
			return currentCount
		}
		for _, item := range raw {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			currentCount++
			entry["id"] = fmt.Sprintf("web:%d", currentCount)
		}
		return currentCount
	}
	for _, entry := range results {
		currentCount++
		entry["id"] = fmt.Sprintf("web:%d", currentCount)
	}
	return currentCount
}
