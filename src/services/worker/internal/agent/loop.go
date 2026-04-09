package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/messagecontent"
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
	Tracer                           pipeline.Tracer
	InputJSON                        map[string]any
	ReasoningIterations              int
	ToolContinuationBudget           int
	MaxParallelToolCalls             int
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
	RunDeadline                      time.Duration
	PausedInputTimeout               time.Duration
	IdleHeartbeatInterval            time.Duration

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
	ctx, cancelDeadline := withRunDeadline(ctx, runCtx.RunDeadline)
	defer cancelDeadline()
	if runCtx.ReasoningIterations < 0 {
		if runCtx.RolloutRecorder != nil {
			appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("failed"))
		}
		return yield(emitter.Emit("run.failed", reasoningIterationsExceededError(runCtx.ReasoningIterations).ToJSON(), nil, stringPtr(ErrorClassAgentReasoningIterationsExceeded)))
	}

	// heartbeat Phase 1: 只暴露 heartbeat_decision 工具，防止模型绕过决策直接调用其他工具
	var heartbeatFullTools []llm.ToolSpec
	if runCtx.PipelineRC != nil &&
		pipeline.IsHeartbeatRunContext(runCtx.PipelineRC) &&
		runCtx.PipelineRC.HeartbeatToolOutcome == nil {
		heartbeatFullTools = append([]llm.ToolSpec{}, request.Tools...)
		var filtered []llm.ToolSpec
		for _, spec := range request.Tools {
			if spec.Name == "heartbeat_decision" {
				filtered = append(filtered, spec)
			}
		}
		request.Tools = filtered
	}

	messages := append([]llm.Message{}, request.Messages...)
	webSourceCount := 0
	seenToolResultKeys := map[string]toolResultDedupInfo{}
	completionTotals := newCompletionTotals()
	reasoningTurnsUsed := 0
	governor := NewLoopGovernor(runCtx)
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
		if terminated, err := governor.Check(ctx, emitter, yield); err != nil {
			return err
		} else if terminated {
			recordRunEnd(ctx, runCtx.RolloutRecorder, "failed")
			return yieldRunDeadlineExceeded(emitter, yield, runCtx)
		}
		if cancelled(runCtx) {
			return yield(emitter.Emit("run.cancelled", completionTotals.Apply(map[string]any{"reason": "cancel_signal"}), nil, nil))
		}

		if runCtx.PreIterHook != nil {
			if err := runCtx.PreIterHook(ctx, turnIndex); err != nil {
				return err
			}
		}

		if runCtx.PollSteeringInput != nil {
			drained, err := drainSteeringMessages(ctx, runCtx.PollSteeringInput, runCtx.UserPromptScanFunc, emitter, yield)
			if err != nil {
				if errors.Is(err, security.ErrInputBlocked) {
					if runCtx.RolloutRecorder != nil {
						appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("cancelled"))
					}
					return nil
				}
				return err
			}
			messages = append(messages, drained...)
			recordRuntimeUserMessages(runCtx.PipelineRC, drained)
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
			governor.Touch()
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
			recordRunEnd(ctx, runCtx.RolloutRecorder, terminalStatusFromTurn(turn, "failed"))
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

		if turn.AssistantText != "" || (turn.AssistantMessage != nil && len(turn.AssistantMessage.Content) > 0) || len(turn.ToolCalls) > 0 {
			assistantMsg := turn.assistantHistoryMessage()
			if runCtx.AgentID == "search" && len(turn.ToolCalls) > 0 {
				assistantMsg.Content = nil
			}
			messages = append(messages, assistantMsg)
		}

		for _, toolResult := range turn.ToolResults {
			messages = append(messages, toolResultMessage(toolResult))
		}

		if turn.CompletedDataJSON == nil {
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("failed"))
			}
			streamErr := llm.InternalStreamEndedError()
			if turnHasRecoverableProgress(turn) {
				streamErr = llm.RetryableStreamEndedError()
			}
			event := emitter.Emit("run.failed", completionTotals.Apply(streamErr.ToJSON()), nil, stringPtr(streamErr.ErrorClass))
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
				drained, err := drainSteeringMessages(ctx, runCtx.PollSteeringInput, runCtx.UserPromptScanFunc, emitter, yield)
				if err != nil {
					if errors.Is(err, security.ErrInputBlocked) {
						if runCtx.RolloutRecorder != nil {
							appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("cancelled"))
						}
						return nil
					}
					return err
				}
				if len(drained) > 0 {
					messages = append(messages, drained...)
					recordRuntimeUserMessages(runCtx.PipelineRC, drained)
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
		terminalSideEffectOnly := len(regularPending) > 0
		terminalSideEffectSucceeded := len(regularPending) > 0
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
				governor.Touch()
				call := executed.Call
				result := executed.Result
				if !isTerminalSideEffectTool(call.ToolName) {
					terminalSideEffectOnly = false
				}
				if result.Error != nil {
					terminalSideEffectSucceeded = false
				}
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

				suppressResultReplay := shouldSuppressToolResultReplay(runCtx, call.ToolName, toolResult.Error == nil)
				if !suppressResultReplay {
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
				}

				var errorClass *string
				if toolResult.Error != nil {
					errorClass = stringPtr(toolResult.Error.ErrorClass)
				}

				// Rollout: 写入 ToolResult
				if runCtx.RolloutRecorder != nil {
					var outputJSON json.RawMessage
					if toolResult.ResultJSON != nil {
						var marshalErr error
						outputJSON, marshalErr = json.Marshal(toolResult.ResultJSON)
						if marshalErr != nil {
							slog.WarnContext(ctx, "rollout: failed to marshal tool result", "tool_call_id", toolResult.ToolCallID, "err", marshalErr)
						}
					}
					errMsg := ""
					if toolResult.Error != nil {
						errMsg = toolResult.Error.Message
					}
					appendRollout(ctx, runCtx.RolloutRecorder, MakeToolResult(toolResult.ToolCallID, outputJSON, errMsg))
				}

				if !suppressResultReplay {
					if err := yield(emitter.Emit("tool.result", toolResult.ToDataJSON(), stringPtr(toolResult.ToolName), errorClass)); err != nil {
						return err
					}
				}
			}
		}

		// 工具执行完成后，检查是否有 steering 消息
		if runCtx.PollSteeringInput != nil {
			drained, err := drainSteeringMessages(ctx, runCtx.PollSteeringInput, runCtx.UserPromptScanFunc, emitter, yield)
			if err != nil {
				if errors.Is(err, security.ErrInputBlocked) {
					if runCtx.RolloutRecorder != nil {
						appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("cancelled"))
					}
					return nil
				}
				return err
			}
			messages = append(messages, drained...)
			recordRuntimeUserMessages(runCtx.PipelineRC, drained)
		}

		// ask_user 拦截：不走 dispatcher，直接 yield 事件并阻塞等待用户输入
		if askUserCall != nil {
			terminalSideEffectOnly = false
			terminalSideEffectSucceeded = false
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
				text, ok, timedOut, waitErr := governor.WaitForUserInput(ctx, emitter, yield, requestID, runCtx.WaitForInput)
				if waitErr != nil {
					return waitErr
				}
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
					if runCtx.PipelineRC != nil {
						runCtx.PipelineRC.AppendRuntimeUserMessage(text)
					}
				} else {
					answerResult = llm.StreamToolResult{
						ToolCallID: requestID,
						ToolName:   askUserToolName,
						ResultJSON: map[string]any{"user_response": "", "dismissed": true, "paused": true},
					}
					if timedOut {
						answerResult.Error = &llm.GatewayError{
							ErrorClass: ErrorClassRunPausedWaitingUser,
							Message:    "waiting for user input timed out",
							Details: map[string]any{
								"request_id": requestID,
								"timeout_ms": runCtx.PausedInputTimeout.Milliseconds(),
							},
						}
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
			if runCtx.RolloutRecorder != nil {
				var outputJSON json.RawMessage
				if answerResult.ResultJSON != nil {
					var marshalErr error
					outputJSON, marshalErr = json.Marshal(answerResult.ResultJSON)
					if marshalErr != nil {
						slog.WarnContext(ctx, "rollout: failed to marshal answer tool result", "tool_call_id", answerResult.ToolCallID, "err", marshalErr)
					}
				}
				errMsg := ""
				if answerResult.Error != nil {
					errMsg = answerResult.Error.Message
				}
				appendRollout(ctx, runCtx.RolloutRecorder, MakeToolResult(answerResult.ToolCallID, outputJSON, errMsg))
			}
			var askErrorClass *string
			if answerResult.Error != nil {
				askErrorClass = stringPtr(answerResult.Error.ErrorClass)
			}
			if err := yield(emitter.Emit("tool.result", answerResult.ToDataJSON(), stringPtr(askUserToolName), askErrorClass)); err != nil {
				return err
			}
		}

		if heartbeatDecisionFinalized(runCtx) {
			if !runCtx.PipelineRC.HeartbeatToolOutcome.Reply {
				reasoningTurnsUsed++
				if runCtx.RolloutRecorder != nil {
					appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("completed"))
				}
				return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
			}
			// reply=true: 解除 tool_choice 约束，恢复完整工具列表
			if request.ToolChoice != nil {
				request.ToolChoice = nil
			}
			if heartbeatFullTools != nil {
				request.Tools = heartbeatFullTools
				heartbeatFullTools = nil
			}
		}
		if terminalSideEffectOnly && terminalSideEffectSucceeded {
			reasoningTurnsUsed++
			if runCtx.RolloutRecorder != nil {
				appendRolloutSync(ctx, runCtx.RolloutRecorder, MakeRunEnd("completed"))
			}
			return yield(emitter.Emit("run.completed", completionTotals.Apply(turn.CompletedDataJSON), nil, nil))
		}

		// load_tools dynamic activation: inject newly activated tool specs
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
				if runCtx.PipelineRC != nil {
					runCtx.PipelineRC.AppendRuntimeUserMessage(injected)
				}
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
	case "run.interrupted":
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

	if len(regularIndexes) == 0 {
		return results
	}

	if l.shouldSerializeToolBatch(runCtx, pending, regularIndexes) {
		for _, idx := range regularIndexes {
			call := pending[idx]
			result := l.executeToolCall(ctx, runCtx, call, emitter, yield)
			results[idx] = pendingToolExecution{Call: call, Result: result}
			if result.Error != nil {
				markSkippedToolCalls(results, pending, regularIndexes, idx+1)
				break
			}
		}
		for _, idx := range regularIndexes {
			updateContinuationTracking(continuation, results[idx].Call, results[idx].Result)
		}
		return results
	}

	parallelism := runCtx.MaxParallelToolCalls
	if parallelism <= 0 {
		parallelism = len(regularIndexes)
	}
	if parallelism > len(regularIndexes) {
		parallelism = len(regularIndexes)
	}

	batchCtx, cancelSiblings := context.WithCancel(ctx)
	defer cancelSiblings()

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	for _, idx := range regularIndexes {
		idx := idx
		call := pending[idx]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-batchCtx.Done():
				resultMu.Lock()
				results[idx] = pendingToolExecution{Call: call, Result: cancelledSiblingToolResult(call)}
				resultMu.Unlock()
				return
			}
			defer func() { <-sem }()

			result := l.executeToolCall(batchCtx, runCtx, call, emitter, yield)
			resultMu.Lock()
			results[idx] = pendingToolExecution{
				Call:   call,
				Result: result,
			}
			resultMu.Unlock()
			if result.Error != nil {
				cancelSiblings()
			}
		}()
	}
	wg.Wait()
	markUnsetParallelResults(results, pending, regularIndexes)
	for _, idx := range regularIndexes {
		updateContinuationTracking(continuation, results[idx].Call, results[idx].Result)
	}
	return results
}

func (l *Loop) shouldSerializeToolBatch(runCtx RunContext, pending []llm.ToolCall, indexes []int) bool {
	if len(indexes) <= 1 || runCtx.ToolExecutor == nil {
		return len(indexes) <= 1
	}
	for _, idx := range indexes {
		capabilities := runCtx.ToolExecutor.ToolCapabilities(pending[idx].ToolName)
		if !capabilities.ConcurrencySafe || capabilities.RequiresExclusiveAccess {
			return true
		}
	}
	return false
}

func cancelledSiblingToolResult(call llm.ToolCall) tools.ExecutionResult {
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"cancelled": true},
		Error: &tools.ExecutionError{
			ErrorClass: "tool.cancelled_by_sibling",
			Message:    "tool cancelled after sibling tool failed",
			Details: map[string]any{
				"tool_name": call.ToolName,
			},
		},
	}
}

func skippedToolResult(call llm.ToolCall) tools.ExecutionResult {
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"skipped": true},
		Error: &tools.ExecutionError{
			ErrorClass: "tool.skipped_after_failure",
			Message:    "tool skipped after an earlier tool failed",
			Details: map[string]any{
				"tool_name": call.ToolName,
			},
		},
	}
}

func markSkippedToolCalls(results []pendingToolExecution, pending []llm.ToolCall, indexes []int, from int) {
	for _, idx := range indexes[from:] {
		results[idx] = pendingToolExecution{
			Call:   pending[idx],
			Result: skippedToolResult(pending[idx]),
		}
	}
}

func markUnsetParallelResults(results []pendingToolExecution, pending []llm.ToolCall, indexes []int) {
	for _, idx := range indexes {
		if results[idx].Call.ToolCallID != "" {
			continue
		}
		results[idx] = pendingToolExecution{
			Call:   pending[idx],
			Result: cancelledSiblingToolResult(pending[idx]),
		}
	}
}

func (l *Loop) executeToolCall(
	ctx context.Context,
	runCtx RunContext,
	call llm.ToolCall,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) tools.ExecutionResult {
	call = llm.CanonicalToolCall(call)
	// Rollout: 写入 ToolCall
	if runCtx.RolloutRecorder != nil {
		inputJSON, _ := json.Marshal(call.ArgumentsJSON)
		appendRollout(ctx, runCtx.RolloutRecorder, MakeToolCall(call.ToolCallID, call.ToolName, inputJSON))
	}

	streamEvent := func(ev events.RunEvent) error {
		return yield(ev)
	}
	var externalSkills []skillstore.ExternalSkill
	if runCtx.PipelineRC != nil {
		externalSkills = append([]skillstore.ExternalSkill(nil), runCtx.PipelineRC.ExternalSkills...)
	}
	execCtx := tools.ExecutionContext{
		RunID:                            runCtx.RunID,
		TraceID:                          runCtx.TraceID,
		Tracer:                           runCtx.Tracer,
		AccountID:                        runCtx.AccountID,
		ThreadID:                         runCtx.ThreadID,
		ProjectID:                        runCtx.ProjectID,
		UserID:                           runCtx.UserID,
		ProfileRef:                       runCtx.ProfileRef,
		WorkspaceRef:                     runCtx.WorkspaceRef,
		WorkDir:                          runCtx.WorkDir,
		EnabledSkills:                    append([]skillstore.ResolvedSkill(nil), runCtx.EnabledSkills...),
		ExternalSkills:                   externalSkills,
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
	if call.ToolName == "continue_process" {
		state.SessionCounts[sessionID] = state.SessionCounts[sessionID] + 1
	}
}

func drainSteeringMessages(
	ctx context.Context,
	poll func(ctx context.Context) (string, bool),
	scan func(context.Context, string, string) error,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) ([]llm.Message, error) {
	if poll == nil {
		return nil, nil
	}
	var out []llm.Message
	for {
		text, ok := poll(ctx)
		if !ok || strings.TrimSpace(text) == "" {
			break
		}
		if scan != nil {
			if err := scan(ctx, text, "steering_input"); err != nil {
				return nil, err
			}
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

func recordRuntimeUserMessages(rc *pipeline.RunContext, messages []llm.Message) {
	if rc == nil {
		return
	}
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		var parts []string
		for _, part := range msg.Content {
			if text := strings.TrimSpace(llm.PartPromptText(part)); text != "" {
				parts = append(parts, text)
			}
		}
		rc.AppendRuntimeUserMessage(strings.Join(parts, "\n"))
	}
}

func trackedSessionID(call llm.ToolCall, result tools.ExecutionResult) string {
	if call.ToolName == "continue_process" {
		return readContinuationSessionRef(call.ArgumentsJSON)
	}
	if result.ResultJSON == nil {
		return ""
	}
	processRef, _ := result.ResultJSON["process_ref"].(string)
	return strings.TrimSpace(processRef)
}

func readContinuationSessionRef(args map[string]any) string {
	if args == nil {
		return ""
	}
	processRef, _ := args["process_ref"].(string)
	return strings.TrimSpace(processRef)
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
		resultJSON["process_ref"] = sessionID
	}
	details := map[string]any{}
	if sessionID != "" {
		details["process_ref"] = sessionID
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
	return toolName == "continue_process"
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
	AssistantMessage  *llm.Message
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

		if turn.Terminal && attempt >= maxAttempts && isRetryableTurn(turn) {
			turn = markTurnInterrupted(turn)
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

func markTurnInterrupted(turn turnResult) turnResult {
	if len(turn.Events) == 0 {
		return turn
	}
	last := turn.Events[len(turn.Events)-1]
	if last.Type != "run.failed" {
		return turn
	}
	last.Type = "run.interrupted"
	turn.Events[len(turn.Events)-1] = last
	return turn
}

func terminalStatusFromTurn(turn turnResult, fallback string) string {
	if len(turn.Events) == 0 {
		return fallback
	}
	switch turn.Events[len(turn.Events)-1].Type {
	case "run.interrupted":
		return "interrupted"
	case "run.failed":
		return "failed"
	case "run.cancelled":
		return "cancelled"
	default:
		return fallback
	}
}

func recordRunEnd(ctx context.Context, recorder *rollout.Recorder, status string) {
	if recorder == nil {
		return
	}
	appendRolloutSync(ctx, recorder, MakeRunEnd(status))
}

// retryBackoffMs 计算指数退避等待时长，最大 60s。
func retryBackoffMs(baseMs, attempt int) int {
	ms := baseMs * (1 << (attempt - 1))
	if ms > 60_000 {
		ms = 60_000
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
	visibleAssistantFilter := assistantControlTokenFilter{}
	var assistantMessage *llm.Message
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

	flushVisibleAssistantTail := func() error {
		tail := visibleAssistantFilter.Flush()
		if tail == "" {
			return nil
		}
		assistantChunks = append(assistantChunks, tail)
		if runCtx.PipelineRC != nil &&
			pipeline.IsHeartbeatRunContext(runCtx.PipelineRC) &&
			runCtx.PipelineRC.HeartbeatToolOutcome == nil {
			return nil
		}
		return yieldOrStop(emitter.Emit("message.delta", llm.StreamMessageDelta{
			ContentDelta: tail,
			Role:         "assistant",
		}.ToDataJSON(), nil, nil))
	}

	err := l.gateway.Stream(ctx, request, func(item llm.StreamEvent) error {
		if cancelled(runCtx) {
			cancelledEarly = true
			return stopErr
		}
		if shouldFlushVisibleAssistantTail(item) {
			if err := flushVisibleAssistantTail(); err != nil {
				return err
			}
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
			// heartbeat Phase 1: outcome 未确定前，累积 context 但不 stream 给客户端
			suppressHeartbeatStream := runCtx.PipelineRC != nil &&
				pipeline.IsHeartbeatRunContext(runCtx.PipelineRC) &&
				runCtx.PipelineRC.HeartbeatToolOutcome == nil
			if typed.Channel == nil {
				cleaned := visibleAssistantFilter.Push(typed.ContentDelta)
				if cleaned == "" {
					return nil
				}
				assistantChunks = append(assistantChunks, cleaned)
				if suppressHeartbeatStream {
					return nil
				}
				return yieldOrStop(emitter.Emit("message.delta", llm.StreamMessageDelta{
					ContentDelta: cleaned,
					Role:         typed.Role,
				}.ToDataJSON(), nil, nil))
			}
			if suppressHeartbeatStream {
				return nil
			}
			return yieldOrStop(emitter.Emit("message.delta", typed.ToDataJSON(), nil, nil))
		case llm.StreamLlmRequest:
			return yieldOrStop(emitter.Emit("llm.request", typed.ToDataJSON(), nil, nil))
		case llm.StreamLlmResponseChunk:
			return yieldOrStop(emitter.Emit("llm.response.chunk", typed.ToDataJSON(), nil, nil))
		case llm.StreamProviderFallback:
			return yieldOrStop(emitter.Emit("run.provider_fallback", typed.ToDataJSON(), nil, nil))
		case llm.ToolCallArgumentDelta:
			typed.ToolName = llm.CanonicalToolName(typed.ToolName)
			return yieldOrStop(emitter.Emit("tool.call.delta", typed.ToDataJSON(), nil, nil))
		case llm.ToolCall:
			typed = llm.CanonicalToolCall(typed)
			toolCalls = append(toolCalls, typed)
			return nil
		case llm.StreamToolResult:
			typed.ToolName = llm.CanonicalToolName(typed.ToolName)
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
			if err := flushVisibleAssistantTail(); err != nil {
				return err
			}
			completed = &typed
			if typed.AssistantMessage != nil {
				copy := *typed.AssistantMessage
				assistantMessage = &copy
			}
			// Rollout: 写入 AssistantMessage
			if runCtx.RolloutRecorder != nil {
				var tcJSON json.RawMessage
				if len(toolCalls) > 0 {
					var marshalErr error
					tcJSON, marshalErr = json.Marshal(toolCalls)
					if marshalErr != nil {
						slog.WarnContext(ctx, "rollout: failed to marshal assistant tool_calls", "err", marshalErr)
					}
				}
				appendRollout(ctx, runCtx.RolloutRecorder, MakeAssistantMessage(llm.VisibleMessageText(assistantMessageOrFallback(assistantMessage, assistantChunks)), tcJSON))
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

	if completed == nil && turnHasRecoverableProgressData(strings.Join(assistantChunks, ""), assistantMessage, toolCalls, toolResults) {
		retryable := llm.RetryableStreamEndedError()
		eventsOut = append(eventsOut, emitter.Emit("run.failed", retryable.ToJSON(), nil, stringPtr(retryable.ErrorClass)))
		if runCtx.RolloutRecorder != nil {
			appendRollout(ctx, runCtx.RolloutRecorder, MakeTurnEnd(turnIndex))
		}
		return turnResult{Events: eventsOut, Terminal: true}, nil
	}

	var completedJSON map[string]any
	if completed != nil {
		completedJSON = completed.ToDataJSON()
		if runCtx.Tracer != nil {
			runCtx.Tracer.Event("agent_loop", "agent_loop.llm_call_completed", map[string]any{
				"model":          request.Model,
				"messages_count": len(request.Messages),
				"tools_count":    len(request.Tools),
				"input_tokens":   traceUsageToken(completedJSON, "input_tokens"),
				"output_tokens":  traceUsageToken(completedJSON, "output_tokens"),
				"tool_calls":     traceTurnToolCalls(toolCalls),
			})
		}
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
		AssistantMessage:  assistantMessage,
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
		ToolChoice:      request.ToolChoice,
		Metadata:        copyMap(request.Metadata),
		ReasoningMode:   request.ReasoningMode,
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

func traceUsageToken(completed map[string]any, key string) int64 {
	if completed == nil {
		return 0
	}
	usage, _ := completed["usage"].(map[string]any)
	if usage == nil {
		return 0
	}
	value, _ := anyToInt64(usage[key])
	return value
}

func traceTurnToolCalls(calls []llm.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	limit := len(calls)
	if limit > 20 {
		limit = 20
	}
	names := make([]string, 0, limit)
	for _, call := range calls[:limit] {
		if name := strings.TrimSpace(call.ToolName); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func turnHasRecoverableProgress(turn turnResult) bool {
	return turnHasRecoverableProgressData(turn.AssistantText, turn.AssistantMessage, turn.ToolCalls, turn.ToolResults)
}

func turnHasRecoverableProgressData(
	assistantText string,
	assistantMessage *llm.Message,
	toolCalls []llm.ToolCall,
	toolResults []llm.StreamToolResult,
) bool {
	return strings.TrimSpace(assistantText) != "" ||
		(assistantMessage != nil && len(assistantMessage.Content) > 0) ||
		len(toolCalls) > 0 ||
		len(toolResults) > 0
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
		callID := extractToolCallIDFromText(m.Content[0].Text)
		stub := map[string]any{
			"tool_call_id": callID,
			"result":       map[string]any{"compacted": true},
		}
		text, _ := json.Marshal(stub)
		return llm.Message{
			Role:    "tool",
			Content: compactedContentParts(m.Content, string(text)),
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
		Content: compactedContentParts(m.Content, string(text)),
	}
}

// compactedContentParts replaces the first text part with stubText
// while preserving non-text parts (images, attachments).
func compactedContentParts(original []llm.ContentPart, stubText string) []llm.ContentPart {
	parts := make([]llm.ContentPart, 0, len(original))
	parts = append(parts, llm.ContentPart{Text: stubText, TrustSource: original[0].TrustSource})
	for _, p := range original[1:] {
		if p.Kind() != "text" {
			parts = append(parts, p)
		}
	}
	return parts
}

// extractToolCallIDFromText attempts to extract a tool_call_id from malformed JSON.
func extractToolCallIDFromText(text string) string {
	prefix := `"tool_call_id":"`
	idx := strings.Index(text, prefix)
	if idx < 0 {
		return "unknown"
	}
	start := idx + len(prefix)
	end := strings.Index(text[start:], `"`)
	if end < 0 {
		return "unknown"
	}
	return text[start : start+end]
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

const assistantReservedControlToken = "<end_turn>"

type assistantControlTokenFilter struct {
	pending string
}

func (f *assistantControlTokenFilter) Push(chunk string) string {
	if chunk == "" {
		return ""
	}
	combined := f.pending + chunk
	f.pending = ""
	if combined == "" {
		return ""
	}
	if suffix := trailingAssistantControlPrefix(combined); suffix != "" {
		f.pending = suffix
		combined = strings.TrimSuffix(combined, suffix)
	}
	if combined == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(combined, assistantReservedControlToken, "")
	if strings.TrimSpace(cleaned) == "" && strings.Contains(combined, assistantReservedControlToken) {
		return ""
	}
	return cleaned
}

func (f *assistantControlTokenFilter) Flush() string {
	tail := f.pending
	f.pending = ""
	return tail
}

func trailingAssistantControlPrefix(text string) string {
	maxSuffix := len(assistantReservedControlToken) - 1
	if len(text) < maxSuffix {
		maxSuffix = len(text)
	}
	for size := maxSuffix; size > 0; size-- {
		suffix := text[len(text)-size:]
		if strings.HasPrefix(assistantReservedControlToken, suffix) {
			return suffix
		}
	}
	return ""
}

func shouldFlushVisibleAssistantTail(item llm.StreamEvent) bool {
	switch item.(type) {
	case llm.StreamMessageDelta, llm.StreamLlmRequest, llm.StreamLlmResponseChunk, llm.StreamSegmentStart, llm.StreamSegmentEnd:
		return false
	default:
		return true
	}
}

func heartbeatDecisionFinalized(runCtx RunContext) bool {
	if runCtx.PipelineRC == nil || !pipeline.IsHeartbeatRunContext(runCtx.PipelineRC) {
		return false
	}
	outcome := runCtx.PipelineRC.HeartbeatToolOutcome
	if outcome == nil {
		return false
	}
	return true
}

func shouldSuppressToolResultReplay(runCtx RunContext, toolName string, success bool) bool {
	if !success {
		return false
	}
	if runCtx.PipelineRC != nil &&
		pipeline.IsHeartbeatRunContext(runCtx.PipelineRC) &&
		toolName == "heartbeat_decision" {
		// reply=true 时 loop 继续，必须保留 tool_result 给下一轮 LLM
		if runCtx.PipelineRC.HeartbeatToolOutcome != nil && runCtx.PipelineRC.HeartbeatToolOutcome.Reply {
			return false
		}
		return true
	}
	return false
}

func isTerminalSideEffectTool(toolName string) bool {
	switch toolName {
	case "telegram_react", "telegram_send_file":
		return true
	default:
		return false
	}
}

func assistantMessageOrFallback(message *llm.Message, assistantChunks []string) llm.Message {
	if message != nil {
		return *message
	}
	content := strings.Join(assistantChunks, "")
	if strings.TrimSpace(content) == "" {
		return llm.Message{Role: "assistant"}
	}
	return llm.Message{
		Role:    "assistant",
		Content: []llm.TextPart{{Text: content}},
	}
}

func (t turnResult) assistantHistoryMessage() llm.Message {
	message := assistantMessageOrFallback(t.AssistantMessage, []string{t.AssistantText})
	message.Role = "assistant"
	message.ToolCalls = llm.CanonicalToolCalls(t.ToolCalls)
	return message
}

func toolResultMessage(result llm.StreamToolResult) llm.Message {
	result.ToolName = llm.CanonicalToolName(result.ToolName)
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
	parts := []llm.ContentPart{
		{Type: messagecontent.PartTypeText, Text: text, TrustSource: "tool"},
	}
	parts = append(parts, result.ContentParts...)
	return llm.Message{
		Role:    "tool",
		Content: parts,
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
	result.ToolName = llm.CanonicalToolName(result.ToolName)
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
	call = llm.CanonicalToolCall(ensureToolCallID(call))
	if dispatcher == nil {
		return call, emitter.Emit("tool.call", call.ToDataJSON(), stringPtr(call.ToolName), nil)
	}
	ev := dispatcher.ToolCallEvent(emitter, call.ToolName, call.ArgumentsJSON, call.ToolCallID)
	if raw, ok := ev.DataJSON["tool_call_id"].(string); ok && strings.TrimSpace(raw) != "" {
		call.ToolCallID = strings.TrimSpace(raw)
	}
	if raw, ok := ev.DataJSON["tool_name"].(string); ok && strings.TrimSpace(raw) != "" {
		call.ToolName = llm.CanonicalToolName(raw)
	}
	return call, ev
}

func toolResultFromExecution(toolCallID string, toolName string, result tools.ExecutionResult) llm.StreamToolResult {
	toolName = llm.CanonicalToolName(toolName)
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
	var contentParts []llm.ContentPart
	for _, att := range result.ContentParts {
		attachment := &messagecontent.AttachmentRef{
			MimeType: att.MimeType,
		}
		if key := strings.TrimSpace(att.AttachmentKey); key != "" {
			attachment.Key = key
		}
		contentParts = append(contentParts, llm.ContentPart{
			Type:       messagecontent.PartTypeImage,
			Data:       att.Data,
			Attachment: attachment,
		})
	}
	return llm.StreamToolResult{
		ToolCallID:   toolCallID,
		ToolName:     toolName,
		ResultJSON:   resultJSON,
		ContentParts: contentParts,
		Error:        errObj,
	}
}

func cancelled(runCtx RunContext) bool {
	if runCtx.CancelSignal == nil {
		return false
	}
	return runCtx.CancelSignal()
}

func withRunDeadline(ctx context.Context, deadline time.Duration) (context.Context, context.CancelFunc) {
	if deadline <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, deadline)
}

func runDeadlineExceeded(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func yieldRunDeadlineExceeded(emitter events.Emitter, yield func(events.RunEvent) error, runCtx RunContext) error {
	return yield(emitter.Emit("run.failed", map[string]any{
		"error_class": ErrorClassRunDeadlineExceeded,
		"message":     "run exceeded wall clock deadline",
		"details": map[string]any{
			"timeout_ms": runCtx.RunDeadline.Milliseconds(),
		},
	}, nil, stringPtr(ErrorClassRunDeadlineExceeded)))
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
