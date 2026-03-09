package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/stablejson"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

const (
	ErrorClassAgentReasoningIterationsExceeded = "agent.reasoning_iterations_exceeded"
	ErrorClassToolContinuationBudgetExceeded   = "tool.continuation_budget_exceeded"
	ErrorClassToolContinuationLimitExceeded    = "tool.continuation_limit_exceeded"
)

type RunContext struct {
	RunID                  uuid.UUID
	OrgID                  *uuid.UUID
	UserID                 *uuid.UUID
	AgentID                string
	ThreadID               *uuid.UUID
	ProjectID              *uuid.UUID
	ProfileRef             string
	WorkspaceRef           string
	TraceID                string
	InputJSON              map[string]any
	ReasoningIterations    int
	ToolContinuationBudget int
	SystemPrompt           string
	MaxOutputTokens        *int
	ToolTimeoutMs          *int
	ToolBudget             map[string]any
	PerToolSoftLimits      tools.PerToolSoftLimits
	ToolExecutor           *tools.DispatchingExecutor
	ToolSpecs              []llm.ToolSpec
	PendingMemoryWrites    *memory.PendingWriteBuffer
	Runtime                *sharedtoolruntime.RuntimeSnapshot
	CancelSignal           func() bool

	// LLM 调用重试配置，0 值表示不重试
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	// IterHook 在每个消耗 reasoning 预算的 turn 完成后被调用。
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
	if runCtx.ReasoningIterations < 0 {
		return yield(emitter.Emit("run.failed", reasoningIterationsExceededError(runCtx.ReasoningIterations).ToJSON(), nil, stringPtr(ErrorClassAgentReasoningIterationsExceeded)))
	}

	messages := append([]llm.Message{}, request.Messages...)
	webSourceCount := 0
	seenToolResultKeys := map[string]toolResultDedupInfo{}
	completionTotals := newCompletionTotals()
	reasoningTurnsUsed := 0
	continuationState := continuationBudgetState{
		Remaining:     maxInt(runCtx.ToolContinuationBudget, 0),
		SessionCounts: map[string]int{},
	}
	for turnIndex := 1; ; turnIndex++ {
		if cancelled(runCtx) {
			return yield(emitter.Emit("run.cancelled", completionTotals.Apply(map[string]any{"reason": "cancel_signal"}), nil, nil))
		}

		if runCtx.PreIterHook != nil {
			if err := runCtx.PreIterHook(ctx, turnIndex); err != nil {
				return err
			}
		}

		turnRequest := copyRequest(request, messages)
		turn, err := l.runTurnWithRetry(ctx, runCtx, turnRequest, emitter, yield)
		if err != nil {
			return err
		}
		if turn.Terminal {
			turn = applyTerminalTotals(turn, completionTotals)
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
			return yield(emitter.Emit("run.cancelled", completionTotals.Apply(map[string]any{"reason": "cancel_signal"}), nil, nil))
		}

		pureContinuationTurn := isPureContinuationTurn(turn.ToolCalls)
		if hasReasoningIterationLimit(runCtx.ReasoningIterations) && !pureContinuationTurn && reasoningTurnsUsed >= runCtx.ReasoningIterations {
			return yield(emitter.Emit("run.failed", completionTotals.Apply(reasoningIterationsExceededError(runCtx.ReasoningIterations).ToJSON()), nil, stringPtr(ErrorClassAgentReasoningIterationsExceeded)))
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
			event := emitter.Emit("run.failed", completionTotals.Apply(internal.ToJSON()), nil, stringPtr(internal.ErrorClass))
			return yield(event)
		}
		completionTotals.Add(turn.CompletedDataJSON)

		if len(turn.ToolCalls) == 0 {
			reasoningTurnsUsed++
			return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
		}

		pending := pendingToolCalls(turn.ToolCalls, turn.ToolResults)
		if len(pending) == 0 {
			if !pureContinuationTurn {
				reasoningTurnsUsed++
			}
			return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
		}

		if cancelled(runCtx) {
			return yield(emitter.Emit("run.cancelled", completionTotals.Apply(map[string]any{"reason": "cancel_signal"}), nil, nil))
		}

		if runCtx.ToolExecutor == nil {
			return fmt.Errorf("tool executor not initialized")
		}
		executedCalls := l.executePendingToolCalls(ctx, runCtx, pending, emitter, &continuationState)
		continuationRejected := false
		for _, executed := range executedCalls {
			call := executed.Call
			result := executed.Result
			if isContinuationBudgetError(result.Error) {
				continuationRejected = true
			}
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

		reasoningUsedThisTurn := !pureContinuationTurn || continuationRejected
		if reasoningUsedThisTurn && pureContinuationTurn && hasReasoningIterationLimit(runCtx.ReasoningIterations) {
			if reasoningTurnsUsed >= runCtx.ReasoningIterations {
				return yield(emitter.Emit("run.failed", completionTotals.Apply(reasoningIterationsExceededError(runCtx.ReasoningIterations).ToJSON()), nil, stringPtr(ErrorClassAgentReasoningIterationsExceeded)))
			}
		}
		if reasoningUsedThisTurn {
			reasoningTurnsUsed++
		}

		// 每个 reasoning turn 完成后，给 InteractiveExecutor 注入用户消息的机会。
		if reasoningUsedThisTurn && runCtx.IterHook != nil {
			injected, inject, hookErr := runCtx.IterHook(ctx, reasoningTurnsUsed)
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
}

func applyTerminalTotals(turn turnResult, totals *completionTotals) turnResult {
	if len(turn.Events) == 0 || totals == nil {
		return turn
	}
	last := turn.Events[len(turn.Events)-1]
	switch last.Type {
	case "run.failed":
		merged := *totals
		merged.Add(last.DataJSON)
		last.DataJSON = merged.Apply(last.DataJSON)
		turn.Events[len(turn.Events)-1] = last
	case "run.cancelled":
		last.DataJSON = totals.Apply(last.DataJSON)
		turn.Events[len(turn.Events)-1] = last
	}
	return turn
}

type pendingToolExecution struct {
	Call   llm.ToolCall
	Result tools.ExecutionResult
}

type continuationBudgetState struct {
	Remaining     int
	SessionCounts map[string]int
}

func (l *Loop) executePendingToolCalls(
	ctx context.Context,
	runCtx RunContext,
	pending []llm.ToolCall,
	emitter events.Emitter,
	continuation *continuationBudgetState,
) []pendingToolExecution {
	results := make([]pendingToolExecution, len(pending))
	regularIndexes := make([]int, 0, len(pending))
	for idx := range pending {
		if isContinuationToolName(pending[idx].ToolName) {
			result := l.executeContinuationToolCall(ctx, runCtx, pending[idx], emitter, continuation)
			results[idx] = pendingToolExecution{Call: pending[idx], Result: result}
			continue
		}
		regularIndexes = append(regularIndexes, idx)
	}

	var wg sync.WaitGroup
	wg.Add(len(regularIndexes))
	for _, idx := range regularIndexes {
		idx := idx
		call := pending[idx]
		go func() {
			defer wg.Done()
			result := l.executeToolCall(ctx, runCtx, call, emitter)
			results[idx] = pendingToolExecution{
				Call:   call,
				Result: result,
			}
		}()
	}
	wg.Wait()
	for _, idx := range regularIndexes {
		updateContinuationTracking(continuation, results[idx].Call, results[idx].Result)
	}
	return results
}

func (l *Loop) executeToolCall(
	ctx context.Context,
	runCtx RunContext,
	call llm.ToolCall,
	emitter events.Emitter,
) tools.ExecutionResult {
	execCtx := tools.ExecutionContext{
		RunID:               runCtx.RunID,
		TraceID:             runCtx.TraceID,
		OrgID:               runCtx.OrgID,
		ThreadID:            runCtx.ThreadID,
		ProjectID:           runCtx.ProjectID,
		UserID:              runCtx.UserID,
		ProfileRef:          runCtx.ProfileRef,
		WorkspaceRef:        runCtx.WorkspaceRef,
		AgentID:             runCtx.AgentID,
		TimeoutMs:           runCtx.ToolTimeoutMs,
		Budget:              copyMap(runCtx.ToolBudget),
		PerToolSoftLimits:   tools.CopyPerToolSoftLimits(runCtx.PerToolSoftLimits),
		Emitter:             emitter,
		PendingMemoryWrites: runCtx.PendingMemoryWrites,
		RuntimeSnapshot:     runCtx.Runtime,
	}
	return runCtx.ToolExecutor.Execute(ctx, call.ToolName, copyMap(call.ArgumentsJSON), execCtx, call.ToolCallID)
}

func (l *Loop) executeContinuationToolCall(
	ctx context.Context,
	runCtx RunContext,
	call llm.ToolCall,
	emitter events.Emitter,
	continuation *continuationBudgetState,
) tools.ExecutionResult {
	sessionID := readContinuationSessionRef(call.ArgumentsJSON)
	if continuation != nil && continuation.Remaining <= 0 {
		result := continuationErrorResult(ErrorClassToolContinuationBudgetExceeded, "tool continuation budget exceeded", sessionID, continuation.Remaining)
		updateContinuationTracking(continuation, call, result)
		return result
	}
	limit := tools.ResolveToolSoftLimit(runCtx.PerToolSoftLimits, call.ToolName)
	if continuation != nil && limit.MaxContinuations != nil && sessionID != "" {
		if continuation.SessionCounts[sessionID] >= *limit.MaxContinuations {
			result := continuationErrorResult(ErrorClassToolContinuationLimitExceeded, "tool continuation limit exceeded", sessionID, *limit.MaxContinuations)
			updateContinuationTracking(continuation, call, result)
			return result
		}
	}
	if continuation != nil {
		continuation.Remaining--
	}
	result := l.executeToolCall(ctx, runCtx, call, emitter)
	updateContinuationTracking(continuation, call, result)
	return result
}

func updateContinuationTracking(state *continuationBudgetState, call llm.ToolCall, result tools.ExecutionResult) {
	if state == nil {
		return
	}
	sessionID := trackedSessionID(call, result)
	if sessionID == "" {
		return
	}
	if result.Error != nil {
		delete(state.SessionCounts, sessionID)
		return
	}
	if !resultRunning(result) {
		delete(state.SessionCounts, sessionID)
		return
	}
	if call.ToolName == "exec_command" {
		state.SessionCounts[sessionID] = 0
		return
	}
	if call.ToolName == "write_stdin" {
		state.SessionCounts[sessionID] = state.SessionCounts[sessionID] + 1
	}
}

func trackedSessionID(call llm.ToolCall, result tools.ExecutionResult) string {
	if call.ToolName == "write_stdin" {
		return readContinuationSessionRef(call.ArgumentsJSON)
	}
	if result.ResultJSON == nil {
		return ""
	}
	sessionID, _ := result.ResultJSON["session_ref"].(string)
	if strings.TrimSpace(sessionID) != "" {
		return strings.TrimSpace(sessionID)
	}
	legacySessionID, _ := result.ResultJSON["session_id"].(string)
	return strings.TrimSpace(legacySessionID)
}

func readContinuationSessionRef(args map[string]any) string {
	if args == nil {
		return ""
	}
	value, _ := args["session_ref"].(string)
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	legacyValue, _ := args["session_id"].(string)
	return strings.TrimSpace(legacyValue)
}

func resultRunning(result tools.ExecutionResult) bool {
	if result.ResultJSON == nil {
		return false
	}
	running, _ := result.ResultJSON["running"].(bool)
	return running
}

func continuationErrorResult(errorClass string, message string, sessionID string, limit int) tools.ExecutionResult {
	resultJSON := map[string]any{"running": false}
	if sessionID != "" {
		resultJSON["session_ref"] = sessionID
	}
	details := map[string]any{}
	if sessionID != "" {
		details["session_ref"] = sessionID
	}
	if limit > 0 {
		details["limit"] = limit
	}
	return tools.ExecutionResult{
		ResultJSON: resultJSON,
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
			Details:    details,
		},
	}
}

func isContinuationToolName(toolName string) bool {
	return toolName == "write_stdin"
}

func isPureContinuationTurn(toolCalls []llm.ToolCall) bool {
	if len(toolCalls) == 0 {
		return false
	}
	for _, call := range toolCalls {
		if !isContinuationToolName(call.ToolName) {
			return false
		}
	}
	return true
}

func isContinuationBudgetError(err *tools.ExecutionError) bool {
	if err == nil {
		return false
	}
	return err.ErrorClass == ErrorClassToolContinuationBudgetExceeded || err.ErrorClass == ErrorClassToolContinuationLimitExceeded
}

func hasReasoningIterationLimit(limit int) bool {
	return limit > 0
}

func reasoningIterationsExceededError(limit int) llm.GatewayError {
	return llm.GatewayError{
		ErrorClass: ErrorClassAgentReasoningIterationsExceeded,
		Message:    "agent loop reached reasoning iteration limit",
		Details:    map[string]any{"reasoning_iterations": limit},
	}
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
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

	if result.Error != nil {
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
