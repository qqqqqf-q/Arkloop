package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/rollout"
	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/security"
	"arkloop/services/worker/internal/stablejson"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/askuser"
	"github.com/google/uuid"
)

const (
	ErrorClassAgentReasoningIterationsExceeded = "agent.reasoning_iterations_exceeded"
	ErrorClassToolContinuationBudgetExceeded   = "tool.continuation_budget_exceeded"
	ErrorClassToolContinuationLimitExceeded    = "tool.continuation_limit_exceeded"

	askUserToolName = "ask_user"
)

type RunContext struct {
	RunID                            uuid.UUID
	AccountID                        *uuid.UUID
	UserID                           *uuid.UUID
	AgentID                          string
	ThreadID                         *uuid.UUID
	ProjectID                        *uuid.UUID
	ProfileRef                       string
	WorkspaceRef                     string
	WorkDir                          string
	EnabledSkills                    []skillstore.ResolvedSkill
	ToolAllowlist                    []string
	ToolDenylist                     []string
	ActiveToolProviderConfigsByGroup map[string]sharedtoolruntime.ProviderConfig
	RouteID                          string
	Model                            string
	MemoryScope                      string
	TraceID                          string
	InputJSON                        map[string]any
	ReasoningIterations              int
	ToolContinuationBudget           int
	SystemPrompt                     string
	MaxOutputTokens                  *int
	ToolTimeoutMs                    *int
	ToolBudget                       map[string]any
	PerToolSoftLimits                tools.PerToolSoftLimits
	MaxCostMicros                    *int64
	MaxTotalOutputTokens             *int64
	ToolExecutor                     *tools.DispatchingExecutor
	ToolSpecs                        []llm.ToolSpec
	PendingMemoryWrites              *memory.PendingWriteBuffer
	Runtime                          *sharedtoolruntime.RuntimeSnapshot
	CancelSignal                     func() bool

	// LLM 调用重试配置，0 值表示不重试
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	// IterHook 在每个消耗 reasoning 预算的 turn 完成后被调用。
	// 返回 (text, true, nil) 时，将 text 作为 user message 注入 messages；nil 时不触发。
	IterHook func(ctx context.Context, iter int) (string, bool, error)

	// PreIterHook 在每轮迭代开始（LLM 调用之前）时被调用。
	PreIterHook func(ctx context.Context, iter int) error

	// WaitForInput 阻塞等待用户输入，供 ask_user 工具使用。
	// 返回 ("", false) 表示超时或取消；返回 (text, true) 表示收到用户输入。
	WaitForInput func(ctx context.Context) (string, bool)

	// PollSteeringInput 非阻塞轮询用户 steering 消息（工具执行后检查）。nil 时不触发。
	PollSteeringInput func(ctx context.Context) (string, bool)

	// UserPromptScanFunc 对运行中追加的人类输入执行 prompt injection 检测。
	UserPromptScanFunc func(ctx context.Context, text string, phase string) error

	// ToolOutputScanFunc 扫描 tool output，检测间接注入。
	// 返回 (sanitized, true) 表示检测到注入；返回 ("", false) 表示安全。
	ToolOutputScanFunc func(toolName, text string) (string, bool)

	Channel *tools.ChannelToolSurface

	// StreamThinking 为 false 时不向客户端下发 channel: thinking 的 message.delta。
	StreamThinking bool

	// PipelineRC 由 agent.simple 注入；Lua 等路径为 nil。
	PipelineRC *pipeline.RunContext

	// RolloutRecorder 用于写入 rollout 日志，为 nil 时不记录
	RolloutRecorder *rollout.Recorder
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
		if runCtx.RolloutRecorder != nil {
			appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("failed"))
		}
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
	var pressureAnchor *pipeline.ContextCompactPressureAnchor
	if runCtx.PipelineRC != nil && runCtx.PipelineRC.HasContextCompactAnchor {
		pressureAnchor = &pipeline.ContextCompactPressureAnchor{
			LastRealPromptTokens:             runCtx.PipelineRC.LastRealPromptTokens,
			LastRequestContextEstimateTokens: runCtx.PipelineRC.LastRequestContextEstimateTokens,
		}
	}

	// Rollout: 写入 RunMeta
	if runCtx.RolloutRecorder != nil {
		appendRollout(ctx, runCtx.RolloutRecorder, MakeRunMeta(runCtx))
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

		if runCtx.PollSteeringInput != nil {
			drained, err := drainSteeringMessages(ctx, runCtx.PollSteeringInput, emitter, yield)
			if err != nil {
				return err
			}
			messages = append(messages, drained...)
		}
		messages = compactToolResults(messages)
		if turnIndex > 1 && runCtx.PipelineRC != nil {
			beforeCompact := len(messages)
			compacted, stats, changed, compactErr := pipeline.MaybeInlineCompactMessages(ctx, runCtx.PipelineRC, messages, pressureAnchor)
			if compactErr != nil {
				ev := emitter.Emit("run.context_compact", map[string]any{
					"op":              "local",
					"phase":           "llm_failed",
					"messages_before": beforeCompact,
					"llm_error":       compactErr.Error(),
				}, nil, nil)
				pipeline.ApplyContextCompactPressureFields(ev.DataJSON, stats)
				if err := yield(ev); err != nil {
					return err
				}
			} else if changed {
				messages = compacted
				ev := emitter.Emit("run.context_compact", map[string]any{
					"op":              "local",
					"phase":           "completed",
					"messages_before": beforeCompact,
					"messages_after":  len(messages),
				}, nil, nil)
				pipeline.ApplyContextCompactPressureFields(ev.DataJSON, stats)
				if err := yield(ev); err != nil {
					return err
				}
			}
		}
		turnRequest := copyRequest(request, messages)
		turnRequestContextEstimateTokens := estimateTurnRequestContextTokens(runCtx, turnRequest.Messages)
		turn, err := l.runTurnWithRetry(ctx, runCtx, turnRequest, emitter, yield, turnIndex)
		if err != nil {
			return err
		}
		if turn.Terminal {
			turn = applyTerminalTotals(turn, completionTotals)
		}

		hasToolCalls := len(turn.ToolCalls) > 0
		for _, event := range turn.Events {
			if event.Type == "message.delta" && !runCtx.StreamThinking {
				if ch, _ := event.DataJSON["channel"].(string); ch == "thinking" {
					continue
				}
			}
			// 当 turn 同时产生了 tool calls 时，只丢弃看起来是 JSON 的非 thinking delta，
			// 保留模型在调用工具前输出的简短说明文本
			if hasToolCalls && event.Type == "message.delta" {
				if ch, _ := event.DataJSON["channel"].(string); ch == "" {
					if text, _ := event.DataJSON["content_delta"].(string); looksLikeJSON(text) {
						continue
					}
				}
			}
			if err := yield(event); err != nil {
				return err
			}
		}

		if turn.Terminal {
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("completed"))
			}
			return nil
		}
		if turn.Cancelled {
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("cancelled"))
			}
			return yield(emitter.Emit("run.cancelled", completionTotals.Apply(map[string]any{"reason": "cancel_signal"}), nil, nil))
		}

		pureContinuationTurn := isPureContinuationTurn(turn.ToolCalls)
		if hasReasoningIterationLimit(runCtx.ReasoningIterations) && !pureContinuationTurn && reasoningTurnsUsed >= runCtx.ReasoningIterations {
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("failed"))
			}
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
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("failed"))
			}
			internal := llm.InternalStreamEndedError()
			event := emitter.Emit("run.failed", completionTotals.Apply(internal.ToJSON()), nil, stringPtr(internal.ErrorClass))
			return yield(event)
		}
		completionTotals.Add(turn.CompletedDataJSON)

		// emit per-turn 完成事件，含 llm_call_id 和本轮 usage
		if turn.CompletedDataJSON != nil {
			attachContextPressureAnchor(turn.CompletedDataJSON, turnRequestContextEstimateTokens)
			if anchor := pressureAnchorFromCompleted(turn.CompletedDataJSON); anchor != nil {
				pressureAnchor = anchor
				if runCtx.PipelineRC != nil {
					runCtx.PipelineRC.SetContextCompactPressureAnchor(
						anchor.LastRealPromptTokens,
						anchor.LastRequestContextEstimateTokens,
					)
				}
			}
			if err := yield(emitter.Emit("llm.turn.completed", turn.CompletedDataJSON, nil, nil)); err != nil {
				return err
			}
		}

		if msg, exceeded := costBudgetExceeded(completionTotals, runCtx.MaxCostMicros, runCtx.MaxTotalOutputTokens); exceeded {
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("failed"))
			}
			return yield(emitter.Emit("run.failed", completionTotals.Apply(costBudgetExceededError(msg)), nil, stringPtr(llm.ErrorClassBudgetExceeded)))
		}

		if len(turn.ToolCalls) == 0 {
			if runCtx.PollSteeringInput != nil {
				drained, err := drainSteeringMessages(ctx, runCtx.PollSteeringInput, emitter, yield)
				if err != nil {
					return err
				}
				if len(drained) > 0 {
					messages = append(messages, drained...)
					continue
				}
			}
			reasoningTurnsUsed++
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("completed"))
			}
			return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
		}

		pending := pendingToolCalls(turn.ToolCalls, turn.ToolResults)
		if len(pending) == 0 {
			if !pureContinuationTurn {
				reasoningTurnsUsed++
			}
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("completed"))
			}
			return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
		}

		if cancelled(runCtx) {
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("cancelled"))
			}
			return yield(emitter.Emit("run.cancelled", completionTotals.Apply(map[string]any{"reason": "cancel_signal"}), nil, nil))
		}

		// 分离 ask_user 调用，先执行其他工具，最后处理 ask_user
		var askUserCall *llm.ToolCall
		regularPending := pending[:0:0]
		for i := range pending {
			if pending[i].ToolName == askUserToolName {
				askUserCall = &pending[i]
			} else {
				regularPending = append(regularPending, pending[i])
			}
		}

		// 执行非 ask_user 的常规工具
		continuationRejected := false
		if len(regularPending) > 0 {
			if runCtx.ToolExecutor == nil {
				return fmt.Errorf("tool executor not initialized")
			}
			preparedPending := make([]llm.ToolCall, 0, len(regularPending))
			for _, pendingCall := range regularPending {
				call, startEvent := prepareToolCallStart(emitter, runCtx.ToolExecutor, pendingCall)
				if err := yield(startEvent); err != nil {
					return err
				}
				preparedPending = append(preparedPending, call)
			}
			executedCalls := l.executePendingToolCalls(ctx, runCtx, preparedPending, emitter, yield, &continuationState)
			for _, executed := range executedCalls {
				call := executed.Call
				result := executed.Result
				if isContinuationBudgetError(result.Error) {
					continuationRejected = true
				}
				if !result.Streamed {
					for _, ev := range result.Events {
						if ev.Type == "tool.call" {
							continue
						}
						if err := yield(ev); err != nil {
							return err
						}
					}
				}

				resolvedID := call.ToolCallID
				toolResult := toolResultFromExecution(resolvedID, call.ToolName, result)

				if call.ToolName == "web_search" {
					webSourceCount = injectWebSourceIDs(toolResult.ResultJSON, webSourceCount)
				}

				if runCtx.ToolOutputScanFunc != nil {
					if err := scanToolOutput(&toolResult, runCtx.ToolOutputScanFunc, emitter, yield); err != nil {
						return err
					}
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

				// Rollout: 写入 ToolResult
				if runCtx.RolloutRecorder != nil {
					var outputJSON json.RawMessage
					if toolResult.ResultJSON != nil {
						outputJSON, _ = json.Marshal(toolResult.ResultJSON)
					}
					errMsg := ""
					if toolResult.Error != nil {
						errMsg = toolResult.Error.Message
					}
					appendRollout(ctx, runCtx.RolloutRecorder, MakeToolResult(toolResult.ToolCallID, outputJSON, errMsg))
				}

				if err := yield(emitter.Emit("tool.result", toolResult.ToDataJSON(), stringPtr(toolResult.ToolName), errorClass)); err != nil {
					return err
				}
			}
		}

		// 工具执行完成后，检查是否有 steering 消息
		if runCtx.PollSteeringInput != nil {
			drained, err := drainSteeringMessages(ctx, runCtx.PollSteeringInput, emitter, yield)
			if err != nil {
				return err
			}
			messages = append(messages, drained...)
		}

		// ask_user 拦截：不走 dispatcher，直接 yield 事件并阻塞等待用户输入
		if askUserCall != nil {
			preparedAskUserCall, startEvent := prepareToolCallStart(emitter, nil, *askUserCall)
			if err := yield(startEvent); err != nil {
				return err
			}

			requestID := preparedAskUserCall.ToolCallID
			callArgs := preparedAskUserCall.ArgumentsJSON

			message, schema, normErr := askuser.ValidateAndNormalize(callArgs)
			if normErr != nil {
				answerResult := llm.StreamToolResult{
					ToolCallID: requestID,
					ToolName:   askUserToolName,
					Error: &llm.GatewayError{
						ErrorClass: "tool.args_invalid",
						Message:    normErr.Error(),
					},
				}
				messages = append(messages, toolResultMessage(answerResult))
				askErrorClass := stringPtr(answerResult.Error.ErrorClass)
				if err := yield(emitter.Emit("tool.result", answerResult.ToDataJSON(), stringPtr(askUserToolName), askErrorClass)); err != nil {
					return err
				}
				continue
			}

			if err := yield(emitter.Emit("run.input_requested", map[string]any{
				"request_id":      requestID,
				"message":         message,
				"requestedSchema": schema,
			}, nil, nil)); err != nil {
				return err
			}

			var answerResult llm.StreamToolResult
			if runCtx.WaitForInput != nil {
				text, ok := runCtx.WaitForInput(ctx)
				if ok && text != "" {
					if runCtx.UserPromptScanFunc != nil {
						if err := runCtx.UserPromptScanFunc(ctx, text, "ask_user"); err != nil {
							if errors.Is(err, security.ErrInputBlocked) {
								return nil
							}
							return err
						}
					}
					var parsed map[string]any
					if err := json.Unmarshal([]byte(text), &parsed); err == nil {
						answerResult = llm.StreamToolResult{
							ToolCallID: requestID,
							ToolName:   askUserToolName,
							ResultJSON: map[string]any{"user_response": parsed},
						}
					} else {
						answerResult = llm.StreamToolResult{
							ToolCallID: requestID,
							ToolName:   askUserToolName,
							ResultJSON: map[string]any{"user_response": text},
						}
					}
				} else {
					answerResult = llm.StreamToolResult{
						ToolCallID: requestID,
						ToolName:   askUserToolName,
						ResultJSON: map[string]any{"user_response": "", "dismissed": true},
					}
				}
			} else {
				answerResult = llm.StreamToolResult{
					ToolCallID: requestID,
					ToolName:   askUserToolName,
					Error: &llm.GatewayError{
						ErrorClass: "tool.not_available",
						Message:    "ask_user requires human-in-the-loop support",
					},
				}
			}

			messages = append(messages, toolResultMessage(answerResult))
			var askErrorClass *string
			if answerResult.Error != nil {
				askErrorClass = stringPtr(answerResult.Error.ErrorClass)
			}
			if err := yield(emitter.Emit("tool.result", answerResult.ToDataJSON(), stringPtr(askUserToolName), askErrorClass)); err != nil {
				return err
			}
		}

		// search_tools dynamic activation: inject newly activated tool specs
		if l.toolExecutor != nil {
			if activated := l.toolExecutor.DrainActivated(); len(activated) > 0 {
				request.Tools = append(request.Tools, activated...)
			}
		}

		reasoningUsedThisTurn := !pureContinuationTurn || continuationRejected
		if reasoningUsedThisTurn && pureContinuationTurn && hasReasoningIterationLimit(runCtx.ReasoningIterations) {
			if reasoningTurnsUsed >= runCtx.ReasoningIterations {
				if runCtx.RolloutRecorder != nil {
					appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("failed"))
				}
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
				if runCtx.UserPromptScanFunc != nil {
					if err := runCtx.UserPromptScanFunc(ctx, injected, "interactive_checkin"); err != nil {
						if errors.Is(err, security.ErrInputBlocked) {
							if runCtx.RolloutRecorder != nil {
								appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("cancelled"))
							}
							return nil
						}
						return err
					}
				}
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
	yield func(events.RunEvent) error,
	continuation *continuationBudgetState,
) []pendingToolExecution {
	results := make([]pendingToolExecution, len(pending))
	regularIndexes := make([]int, 0, len(pending))
	for idx := range pending {
		if isContinuationToolName(pending[idx].ToolName) {
			result := l.executeContinuationToolCall(ctx, runCtx, pending[idx], emitter, yield, continuation)
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
			result := l.executeToolCall(ctx, runCtx, call, emitter, yield)
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
	yield func(events.RunEvent) error,
) tools.ExecutionResult {
	// Rollout: 写入 ToolCall
	if runCtx.RolloutRecorder != nil {
		inputJSON, _ := json.Marshal(call.ArgumentsJSON)
		appendRollout(ctx, runCtx.RolloutRecorder, MakeToolCall(call.ToolCallID, call.ToolName, inputJSON))
	}

	streamEvent := func(ev events.RunEvent) error {
		return yield(ev)
	}
	execCtx := tools.ExecutionContext{
		RunID:                            runCtx.RunID,
		TraceID:                          runCtx.TraceID,
		AccountID:                        runCtx.AccountID,
		ThreadID:                         runCtx.ThreadID,
		ProjectID:                        runCtx.ProjectID,
		UserID:                           runCtx.UserID,
		ProfileRef:                       runCtx.ProfileRef,
		WorkspaceRef:                     runCtx.WorkspaceRef,
		WorkDir:                          runCtx.WorkDir,
		EnabledSkills:                    append([]skillstore.ResolvedSkill(nil), runCtx.EnabledSkills...),
		ToolAllowlist:                    append([]string(nil), runCtx.ToolAllowlist...),
		ToolDenylist:                     append([]string(nil), runCtx.ToolDenylist...),
		ActiveToolProviderConfigsByGroup: copyProviderConfigs(runCtx.ActiveToolProviderConfigsByGroup),
		RouteID:                          runCtx.RouteID,
		Model:                            runCtx.Model,
		MemoryScope:                      runCtx.MemoryScope,
		AgentID:                          runCtx.AgentID,
		TimeoutMs:                        runCtx.ToolTimeoutMs,
		Budget:                           copyMap(runCtx.ToolBudget),
		PerToolSoftLimits:                tools.CopyPerToolSoftLimits(runCtx.PerToolSoftLimits),
		Emitter:                          emitter,
		PendingMemoryWrites:              runCtx.PendingMemoryWrites,
		RuntimeSnapshot:                  runCtx.Runtime,
		Channel:                          runCtx.Channel,
		PipelineRC:                       runCtx.PipelineRC,
		StreamEvent:                      streamEvent,
	}
	return runCtx.ToolExecutor.Execute(ctx, call.ToolName, copyMap(call.ArgumentsJSON), execCtx, call.ToolCallID)
}

func copyProviderConfigs(src map[string]sharedtoolruntime.ProviderConfig) map[string]sharedtoolruntime.ProviderConfig {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]sharedtoolruntime.ProviderConfig, len(src))
	for group, cfg := range src {
		out[group] = sharedtoolruntime.ProviderConfig{
			GroupName:    cfg.GroupName,
			ProviderName: cfg.ProviderName,
			BaseURL:      cfg.BaseURL,
			APIKeyValue:  cfg.APIKeyValue,
			ConfigJSON:   copyProviderConfigJSON(cfg.ConfigJSON),
		}
	}
	return out
}

func copyProviderConfigJSON(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (l *Loop) executeContinuationToolCall(
	ctx context.Context,
	runCtx RunContext,
	call llm.ToolCall,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
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
	result := l.executeToolCall(ctx, runCtx, call, emitter, yield)
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

func drainSteeringMessages(ctx context.Context, poll func(ctx context.Context) (string, bool), emitter events.Emitter, yield func(events.RunEvent) error) ([]llm.Message, error) {
	if poll == nil {
		return nil, nil
	}
	var out []llm.Message
	for {
		text, ok := poll(ctx)
		if !ok || strings.TrimSpace(text) == "" {
			break
		}
		msg := llm.Message{
			Role:    "user",
			Content: []llm.TextPart{{Text: text}},
		}
		out = append(out, msg)
		if err := yield(emitter.Emit("run.steering_injected", map[string]any{"content": text}, nil, nil)); err != nil {
			return nil, err
		}
	}
	return out, nil
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

func costBudgetExceeded(totals *completionTotals, maxCostMicros *int64, maxTotalOutputTokens *int64) (string, bool) {
	if maxCostMicros != nil && *maxCostMicros > 0 && totals.hasCostMicros && totals.costMicros > *maxCostMicros {
		return fmt.Sprintf("cost budget exceeded: %d/%d micros", totals.costMicros, *maxCostMicros), true
	}
	if maxTotalOutputTokens != nil && *maxTotalOutputTokens > 0 && totals.hasOutputTokens && totals.outputTokens > *maxTotalOutputTokens {
		return fmt.Sprintf("output token budget exceeded: %d/%d tokens", totals.outputTokens, *maxTotalOutputTokens), true
	}
	return "", false
}

func costBudgetExceededError(message string) map[string]any {
	return map[string]any{
		"error_class": llm.ErrorClassBudgetExceeded,
		"message":     message,
	}
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
	turnIndex int,
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
		turn, err := l.runSingleTurn(ctx, runCtx, turnRequest, emitter, yield, turnIndex)
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
	yield func(events.RunEvent) error,
	turnIndex int,
) (turnResult, error) {
	eventsOut := []events.RunEvent{}
	toolCalls := []llm.ToolCall{}
	toolResults := []llm.StreamToolResult{}
	assistantChunks := []string{}
	var completed *llm.StreamRunCompleted

	// Rollout: 写入 TurnStart
	if runCtx.RolloutRecorder != nil {
		appendRollout(ctx, runCtx.RolloutRecorder, MakeTurnStart(turnIndex, request.Model))
	}

	cancelledEarly := false
	stopErr := fmt.Errorf("stop")

	streamingEventTypes := map[string]struct{}{
		"llm.request":        {},
		"message.delta":      {},
		"llm.response.chunk": {},
		"run.segment.start":  {},
		"run.segment.end":    {},
		"tool.call.delta":    {},
	}

	yieldOrStop := func(ev events.RunEvent) error {
		if _, isStreaming := streamingEventTypes[ev.Type]; isStreaming {
			return yield(ev)
		}
		eventsOut = append(eventsOut, ev)
		return nil
	}

	err := l.gateway.Stream(ctx, request, func(item llm.StreamEvent) error {
		if cancelled(runCtx) {
			cancelledEarly = true
			return stopErr
		}

		switch typed := item.(type) {
		case llm.StreamSegmentStart:
			return yieldOrStop(emitter.Emit("run.segment.start", typed.ToDataJSON(), nil, nil))
		case llm.StreamSegmentEnd:
			return yieldOrStop(emitter.Emit("run.segment.end", typed.ToDataJSON(), nil, nil))
		case llm.StreamMessageDelta:
			if typed.ContentDelta == "" {
				return nil
			}
			if typed.Channel != nil && *typed.Channel == "thinking" && !runCtx.StreamThinking {
				return nil
			}
			// thinking 内容不计入对话上下文
			if typed.Channel == nil {
				assistantChunks = append(assistantChunks, typed.ContentDelta)
			}
			return yieldOrStop(emitter.Emit("message.delta", typed.ToDataJSON(), nil, nil))
		case llm.StreamLlmRequest:
			return yieldOrStop(emitter.Emit("llm.request", typed.ToDataJSON(), nil, nil))
		case llm.StreamLlmResponseChunk:
			return yieldOrStop(emitter.Emit("llm.response.chunk", typed.ToDataJSON(), nil, nil))
		case llm.StreamProviderFallback:
			return yieldOrStop(emitter.Emit("run.provider_fallback", typed.ToDataJSON(), nil, nil))
		case llm.ToolCallArgumentDelta:
			return yieldOrStop(emitter.Emit("tool.call.delta", typed.ToDataJSON(), nil, nil))
		case llm.ToolCall:
			toolCalls = append(toolCalls, typed)
			return nil
		case llm.StreamToolResult:
			toolResults = append(toolResults, typed)
			var errorClass *string
			if typed.Error != nil {
				errorClass = stringPtr(typed.Error.ErrorClass)
			}
			return yieldOrStop(emitter.Emit("tool.result", typed.ToDataJSON(), stringPtr(typed.ToolName), errorClass))
		case llm.StreamRunFailed:
			errorClass := stringPtr(typed.Error.ErrorClass)
			eventsOut = append(eventsOut, emitter.Emit("run.failed", typed.ToDataJSON(), nil, errorClass))
			return stopErr
		case llm.StreamRunCompleted:
			completed = &typed
			// Rollout: 写入 AssistantMessage
			if runCtx.RolloutRecorder != nil {
				var tcJSON json.RawMessage
				if len(toolCalls) > 0 {
					tcJSON, _ = json.Marshal(toolCalls)
				}
				appendRollout(ctx, runCtx.RolloutRecorder, MakeAssistantMessage(strings.Join(assistantChunks, ""), tcJSON))
			}
			return stopErr
		default:
			return fmt.Errorf("unknown LLM gateway event type: %T", item)
		}
	})
	if err != nil && err != stopErr {
		return turnResult{}, err
	}

	if cancelledEarly {
		if runCtx.RolloutRecorder != nil {
			appendRollout(ctx, runCtx.RolloutRecorder, MakeTurnEnd(turnIndex))
		}
		return turnResult{Events: eventsOut, Cancelled: true}, nil
	}

	if len(eventsOut) > 0 {
		last := eventsOut[len(eventsOut)-1]
		if last.Type == "run.failed" {
			if runCtx.RolloutRecorder != nil {
				appendRollout(ctx, runCtx.RolloutRecorder, MakeTurnEnd(turnIndex))
			}
			return turnResult{Events: eventsOut, Terminal: true}, nil
		}
	}

	var completedJSON map[string]any
	if completed != nil {
		completedJSON = completed.ToDataJSON()
	}

	// Rollout: 写入 TurnEnd
	if runCtx.RolloutRecorder != nil {
		appendRollout(ctx, runCtx.RolloutRecorder, MakeTurnEnd(turnIndex))
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

func estimateTurnRequestContextTokens(runCtx RunContext, messages []llm.Message) int {
	if runCtx.PipelineRC != nil {
		return pipeline.HistoryThreadPromptTokensForRoute(runCtx.PipelineRC.SelectedRoute, messages)
	}
	return pipeline.HistoryThreadPromptTokensForRoute(nil, messages)
}

func attachContextPressureAnchor(data map[string]any, requestEstimateTokens int) {
	if data == nil || requestEstimateTokens < 0 {
		return
	}
	data["last_request_context_estimate_tokens"] = requestEstimateTokens
	usage, _ := data["usage"].(map[string]any)
	if usage == nil {
		return
	}
	if inputTokens, ok := anyToInt64(usage["input_tokens"]); ok && inputTokens > 0 {
		data["last_real_prompt_tokens"] = inputTokens
	}
}

func pressureAnchorFromCompleted(data map[string]any) *pipeline.ContextCompactPressureAnchor {
	if data == nil {
		return nil
	}
	lastRealPromptTokens, ok := anyToInt64(data["last_real_prompt_tokens"])
	if !ok || lastRealPromptTokens <= 0 {
		return nil
	}
	lastRequestEstimate, ok := anyToInt64(data["last_request_context_estimate_tokens"])
	if !ok || lastRequestEstimate < 0 {
		return nil
	}
	return &pipeline.ContextCompactPressureAnchor{
		LastRealPromptTokens:             int(lastRealPromptTokens),
		LastRequestContextEstimateTokens: int(lastRequestEstimate),
	}
}

// maxToolResultHistoryChars is the soft cap on total accumulated tool result text
// sent in a single LLM request. At ~4 chars/token this is ≈20K tokens.
// Oldest tool results are compacted first when the cap is exceeded.
const maxToolResultHistoryChars = 80_000

// compactToolResults returns a copy of messages where the oldest tool result
// messages are replaced by minimal placeholders if the total tool result
// character count exceeds maxToolResultHistoryChars.
// The original messages slice is never modified.
func compactToolResults(messages []llm.Message) []llm.Message {
	total := 0
	for _, m := range messages {
		if m.Role == "tool" {
			for _, p := range m.Content {
				total += len(p.Text)
			}
		}
	}
	if total <= maxToolResultHistoryChars {
		return messages
	}

	out := make([]llm.Message, len(messages))
	copy(out, messages)

	excess := total - maxToolResultHistoryChars
	for i := range out {
		if excess <= 0 {
			break
		}
		if out[i].Role != "tool" {
			continue
		}
		msgSize := 0
		for _, p := range out[i].Content {
			msgSize += len(p.Text)
		}
		if msgSize == 0 {
			continue
		}
		out[i] = compactedToolMessage(out[i])
		excess -= msgSize
	}
	return out
}

// compactedToolMessage returns a minimal version of a tool result message,
// preserving only the tool_call_id so the conversation structure stays valid.
func compactedToolMessage(m llm.Message) llm.Message {
	if len(m.Content) == 0 {
		return m
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(m.Content[0].Text), &envelope); err != nil {
		// unparseable: emit a safe stub
		return llm.Message{
			Role:    "tool",
			Content: []llm.TextPart{{Text: `{"tool_call_id":"","result":{"compacted":true}}`, TrustSource: m.Content[0].TrustSource}},
		}
	}
	toolName, _ := envelope["tool_name"].(string)
	if tools.IsGenerativeUIBootstrapTool(toolName) {
		return m
	}
	stub := map[string]any{
		"tool_call_id": envelope["tool_call_id"],
		"result":       map[string]any{"compacted": true},
	}
	if strings.TrimSpace(toolName) != "" {
		stub["tool_name"] = strings.TrimSpace(toolName)
	}
	text, _ := json.Marshal(stub)
	return llm.Message{
		Role:    "tool",
		Content: []llm.TextPart{{Text: string(text), TrustSource: m.Content[0].TrustSource}},
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
		Content: []llm.TextPart{{Text: text, TrustSource: "tool"}},
	}
}

// scanToolOutput 扫描 tool output 是否包含间接注入。
// 检测到注入时用消毒后的内容替换 ResultJSON，并发出事件。
func scanToolOutput(
	result *llm.StreamToolResult,
	scanFunc func(string, string) (string, bool),
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) error {
	if result.Error != nil || result.ResultJSON == nil {
		return nil
	}
	text := collectToolOutputScanText(result.ResultJSON)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	sanitized, detected := scanFunc(result.ToolName, text)
	if !detected {
		return nil
	}
	result.ResultJSON = map[string]any{
		"warning":            "indirect injection detected, content sanitized",
		"sanitized_content":  sanitized,
		"original_tool_name": result.ToolName,
	}
	return yield(emitter.Emit("security.tool_injection.detected", map[string]any{
		"tool_name": result.ToolName,
	}, nil, nil))
}

func collectToolOutputScanText(result map[string]any) string {
	raw, err := json.Marshal(result)
	if err != nil {
		return ""
	}

	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return ""
	}

	seen := map[string]struct{}{}
	parts := collectToolOutputStrings(nil, normalized, seen)
	return strings.Join(parts, "\n\n")
}

func collectToolOutputStrings(parts []string, value any, seen map[string]struct{}) []string {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return parts
		}
		if _, ok := seen[text]; ok {
			return parts
		}
		seen[text] = struct{}{}
		return append(parts, text)
	case []any:
		for _, item := range typed {
			parts = collectToolOutputStrings(parts, item, seen)
		}
		return parts
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = collectToolOutputStrings(parts, typed[key], seen)
		}
	}
	return parts
}

type toolResultDedupInfo struct {
	ToolCallID string
	Signature  string
}

func toolResultDedupKey(toolName string, args map[string]any, result llm.StreamToolResult) (string, string, bool) {
	if strings.TrimSpace(toolName) == "" || args == nil {
		return "", "", false
	}
	if tools.IsGenerativeUIBootstrapTool(toolName) {
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
		"tool_name":    result.ToolName,
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
		Content: []llm.TextPart{{Text: text, TrustSource: "tool"}},
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

func ensureToolCallID(call llm.ToolCall) llm.ToolCall {
	if strings.TrimSpace(call.ToolCallID) != "" {
		return call
	}
	call.ToolCallID = uuid.NewString()
	return call
}

func prepareToolCallStart(
	emitter events.Emitter,
	dispatcher *tools.DispatchingExecutor,
	call llm.ToolCall,
) (llm.ToolCall, events.RunEvent) {
	call = ensureToolCallID(call)
	if dispatcher == nil {
		return call, emitter.Emit("tool.call", call.ToDataJSON(), stringPtr(call.ToolName), nil)
	}
	ev := dispatcher.ToolCallEvent(emitter, call.ToolName, call.ArgumentsJSON, call.ToolCallID)
	if raw, ok := ev.DataJSON["tool_call_id"].(string); ok && strings.TrimSpace(raw) != "" {
		call.ToolCallID = strings.TrimSpace(raw)
	}
	return call, ev
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

// looksLikeJSON 判断文本是否疑似工具参数 echo（JSON 片段），
// 用于在 preamble 过滤中区分正常说明文字和 JSON 内容
func looksLikeJSON(text string) bool {
	t := strings.TrimSpace(text)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}
