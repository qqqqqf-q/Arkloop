package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"arkloop/services/worker_go/internal/events"
	"arkloop/services/worker_go/internal/llm"
	"arkloop/services/worker_go/internal/stablejson"
	"arkloop/services/worker_go/internal/tools"
	"github.com/google/uuid"
)

const ErrorClassAgentMaxIterationsExceeded = "agent.max_iterations_exceeded"

type RunContext struct {
	RunID          uuid.UUID
	TraceID        string
	InputJSON      map[string]any
	MaxIterations  int
	SystemPrompt   string
	MaxOutputTokens *int
	ToolTimeoutMs  *int
	ToolBudget     map[string]any
	ToolExecutor   *tools.DispatchingExecutor
	ToolSpecs      []llm.ToolSpec
	CancelSignal   func() bool
}

type Loop struct {
	gateway       llm.Gateway
	toolExecutor  *tools.DispatchingExecutor
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
			Message:    "Agent 循环达到最大轮次",
			Details:    map[string]any{"max_iterations": runCtx.MaxIterations},
		}
		event := emitter.Emit("run.failed", errPayload.ToJSON(), nil, stringPtr(errPayload.ErrorClass))
		return yield(event)
	}

	messages := append([]llm.Message{}, request.Messages...)
	for iter := 1; iter <= runCtx.MaxIterations; iter++ {
		if cancelled(runCtx) {
			return yield(emitter.Emit("run.cancelled", map[string]any{"reason": "cancel_signal"}, nil, nil))
		}

		turnRequest := copyRequest(request, messages)
		turn, err := l.runSingleTurn(ctx, runCtx, turnRequest, emitter)
		if err != nil {
			return err
		}

		for _, event := range turn.Events {
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
			messages = append(messages, assistantMessage(turn.AssistantText, turn.ToolCalls))
		}

		for _, toolResult := range turn.ToolResults {
			messages = append(messages, toolResultMessage(toolResult))
		}

		if turn.CompletedDataJSON == nil {
			internal := llm.InternalStreamEndedError()
			event := emitter.Emit("run.failed", internal.ToJSON(), nil, stringPtr(internal.ErrorClass))
			_ = yield(event)
			return nil
		}

		if len(turn.ToolCalls) == 0 {
			return yield(emitter.Emit("run.completed", mapOrEmpty(turn.CompletedDataJSON), nil, nil))
		}

		pending := pendingToolCalls(turn.ToolCalls, turn.ToolResults)
		if len(pending) == 0 {
			return yield(emitter.Emit("run.completed", mapOrEmpty(turn.CompletedDataJSON), nil, nil))
		}

		if cancelled(runCtx) {
			return yield(emitter.Emit("run.cancelled", map[string]any{"reason": "cancel_signal"}, nil, nil))
		}

		for _, call := range pending {
			if runCtx.ToolExecutor == nil {
				return fmt.Errorf("tool executor 未初始化")
			}

			execCtx := tools.ExecutionContext{
				RunID:     runCtx.RunID,
				TraceID:   runCtx.TraceID,
				TimeoutMs: runCtx.ToolTimeoutMs,
				Budget:    copyMap(runCtx.ToolBudget),
				Emitter:   emitter,
			}
			result := runCtx.ToolExecutor.Execute(ctx, call.ToolName, copyMap(call.ArgumentsJSON), execCtx, call.ToolCallID)

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
			messages = append(messages, toolResultMessage(toolResult))

			var errorClass *string
			if toolResult.Error != nil {
				errorClass = stringPtr(toolResult.Error.ErrorClass)
			}
			if err := yield(emitter.Emit("tool.result", toolResult.ToDataJSON(), stringPtr(toolResult.ToolName), errorClass)); err != nil {
				return err
			}
		}
	}

	errPayload := llm.GatewayError{
		ErrorClass: ErrorClassAgentMaxIterationsExceeded,
		Message:    "Agent 循环达到最大轮次",
		Details:    map[string]any{"max_iterations": runCtx.MaxIterations},
	}
	return yield(emitter.Emit("run.failed", errPayload.ToJSON(), nil, stringPtr(errPayload.ErrorClass)))
}

type turnResult struct {
	Events           []events.RunEvent
	Terminal         bool
	Cancelled        bool
	ToolCalls        []llm.ToolCall
	ToolResults      []llm.StreamToolResult
	AssistantText    string
	CompletedDataJSON map[string]any
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
		case llm.StreamMessageDelta:
			if strings.TrimSpace(typed.ContentDelta) == "" {
				return nil
			}
			assistantChunks = append(assistantChunks, typed.ContentDelta)
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
			return fmt.Errorf("未知的 LLM Gateway 事件类型: %T", item)
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
		Model:          request.Model,
		Messages:       append([]llm.Message{}, messages...),
		Temperature:    request.Temperature,
		MaxOutputTokens: request.MaxOutputTokens,
		Tools:          append([]llm.ToolSpec{}, request.Tools...),
		Metadata:       copyMap(request.Metadata),
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
		"tool_name":    result.ToolName,
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
	return llm.StreamToolResult{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		ResultJSON: copyMap(result.ResultJSON),
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

