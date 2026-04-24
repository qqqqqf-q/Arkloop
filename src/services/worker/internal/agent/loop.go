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
	"arkloop/services/worker/internal/subagentctl"
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

	// CacheSafeSnapshot stores cache-key-critical request pieces for
	// fork/subagent cache-chain reuse.
	CacheSafeSnapshot *CacheSafeSnapshot

	// RolloutRecorder 用于写入 rollout 日志，为 nil 时不记录
	RolloutRecorder *rollout.Recorder
}

type promptCacheTurnState struct {
	PreviousStableHash   string
	PreviousSessionHash  string
	PreviousVolatileHash string
	PreviousToolHash     string
	PreviousStableBytes  int
	KnownToolResultRefs  map[string]struct{}
	PinnedCacheEdits     []llm.PromptCacheEditsBlock
	StableMarkerIndex    int  // -1 = not yet set; pinned after first turn
	StableMarkerPinned   bool // true after first turn sets the stable marker
}

type toolResultReplacementState struct {
	ByToolCallID map[string]string
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
	toolResultReplacements := &toolResultReplacementState{ByToolCallID: map[string]string{}}
	var promptCacheState *promptCacheTurnState
	if promptCacheEnabled(runCtx) {
		promptCacheState = &promptCacheTurnState{
			KnownToolResultRefs: map[string]struct{}{},
		}
	}
	continuationState := continuationBudgetState{
		Remaining:     maxInt(runCtx.ToolContinuationBudget, 0),
		SessionCounts: map[string]int{},
	}
	var prevTurnUsage map[string]any
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
		messages = compactToolResultsWithState(messages, toolResultReplacements)
		turnRequest := copyRequest(request, messages)
		debugEnabled := runCtx.PipelineRC != nil && runCtx.PipelineRC.PromptCacheDebugEnabled

		if promptCacheState != nil {
			prepareTurnRequestPromptCache(&turnRequest, runCtx, promptCacheState)
		}
		// computePromptCacheBreak 纯计算：得出 break info 与 stats，并将 state 推进到当前轮
		var breakInfo promptCacheBreakInfo
		var requestStats llm.RequestStats
		if promptCacheState != nil {
			breakInfo, requestStats = computePromptCacheBreak(turnRequest, promptCacheState)
		} else {
			requestStats = llm.ComputeRequestStats(turnRequest)
		}
		// emit 调试事件：在 LLM 调用之前。markers 从 plan 计算，与 stream 成败解耦。
		if debugEnabled {
			markers := computePlanMarkers(turnRequest)
			payload := buildPromptCacheDebugPayload(
				runCtx.PipelineRC,
				turnIndex,
				promptCacheEnabled(runCtx),
				requestStats,
				markers,
				breakInfo,
				prevTurnUsage,
			)
			if err := yield(emitter.Emit("run.prompt_cache_debug", payload, nil, nil)); err != nil {
				return err
			}
		}

		refreshCacheSafeSnapshot(&runCtx, messages, turnRequest)
		turnRequestContextEstimateTokens := estimateTurnRequestContextTokens(runCtx, turnRequest)
		if runCtx.PipelineRC != nil && runCtx.PipelineRC.HookRuntime != nil && runCtx.PipelineRC.HookRegistry != nil {
			runCtx.PipelineRC.HookRuntime.BeforeModelCall(ctx, runCtx.PipelineRC, turnRequest)
		}
		turn, err := l.runTurnWithRetry(ctx, runCtx, turnRequest, emitter, yield, turnIndex)
		if err != nil {
			return err
		}
		if runCtx.PipelineRC != nil && runCtx.PipelineRC.HookRuntime != nil && runCtx.PipelineRC.HookRegistry != nil {
			runCtx.PipelineRC.HookRuntime.AfterModelResponse(ctx, runCtx.PipelineRC, pipeline.ModelResponse{
				AssistantText: strings.TrimSpace(turn.AssistantText),
				ToolCalls:     append([]llm.ToolCall(nil), turn.ToolCalls...),
				ToolResults:   append([]llm.StreamToolResult(nil), turn.ToolResults...),
				Completed:     copyMap(turn.CompletedDataJSON),
				Terminal:      turn.Terminal,
				Cancelled:     turn.Cancelled,
			})
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
			prevTurnUsage = copyMap(turn.CompletedDataJSON)
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
				if runCtx.PipelineRC != nil && runCtx.PipelineRC.HookRuntime != nil && runCtx.PipelineRC.HookRegistry != nil {
					runCtx.PipelineRC.HookRuntime.AfterToolCall(ctx, runCtx.PipelineRC, call, result)
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

		// end_reply: terminate run without further output
		if runCtx.PipelineRC != nil && runCtx.PipelineRC.EndReplyRequested {
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
		PersonaID:                        loopPersonaID(runCtx),
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
		PromptCacheSnapshot:              promptCacheSnapshotFromLoopContext(runCtx),
		Channel:                          runCtx.Channel,
		PipelineRC:                       runCtx.PipelineRC,
		StreamEvent:                      streamEvent,
	}
	result := runCtx.ToolExecutor.Execute(ctx, call.ToolName, copyMap(call.ArgumentsJSON), execCtx, call.ToolCallID)
	return result
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
	var texts []string
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
		texts = append(texts, strings.TrimSpace(text))
	}
	var out []llm.Message
	if len(texts) <= 1 {
		out = pipeline.NormalizeRuntimeSteeringInputs(texts)
	} else {
		out = make([]llm.Message, 0, len(texts))
		for _, text := range texts {
			out = append(out, llm.Message{
				Role:    "user",
				Content: []llm.TextPart{{Text: text}},
			})
		}
	}
	for _, msg := range out {
		content := strings.TrimSpace(llm.VisibleMessageText(msg))
		if content == "" {
			continue
		}
		if err := yield(emitter.Emit("run.steering_injected", map[string]any{"content": content}, nil, nil)); err != nil {
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
	currentRequest := turnRequest
	sizeEstimator := newRequestSizeEstimator(runCtx)
	if !sizeEstimator.Available {
		return requestEstimatorFailureTurnResult(emitter, sizeEstimator.Err), nil
	}
	providerRecoveryUsed := false

	for attempt := 1; attempt <= maxAttempts; {
		preflightRecovered := false
		for preflightRound := 0; ; preflightRound++ {
			payloadBytes, err := sizeEstimator.Estimate(currentRequest)
			if err != nil {
				return requestEstimatorFailureTurnResult(emitter, err), nil
			}
			contextWindowTokens := 0
			if runCtx.PipelineRC != nil {
				contextWindowTokens = pipeline.ResolveRunContextWindowTokens(runCtx.PipelineRC)
			}
			estimatedTokens := estimateTurnRequestLimitTokens(runCtx, currentRequest)
			if !llm.RequestExceedsLimits(payloadBytes, estimatedTokens, contextWindowTokens) {
				break
			}
			rewritten, recovered, currentInputErr, err := maybeRecoverOversizeRequest(
				ctx,
				runCtx,
				currentRequest,
				nil,
				emitter,
				yield,
				turnIndex,
				"preflight",
				sizeEstimator.Estimate,
			)
			if err != nil {
				return turnResult{}, err
			}
			if currentInputErr != nil {
				return currentInputOversizeTurnResult(emitter, currentInputErr), nil
			}
			if !recovered {
				if llm.RequestPayloadTooLarge(payloadBytes) {
					return preflightOversizeTurnResult(emitter, payloadBytes, estimatedTokens, contextWindowTokens), nil
				}
				break
			}
			rewrittenBytes, err := sizeEstimator.Estimate(rewritten)
			if err != nil {
				return requestEstimatorFailureTurnResult(emitter, err), nil
			}
			rewrittenTokens := estimateTurnRequestLimitTokens(runCtx, rewritten)
			if rewrittenBytes >= payloadBytes && rewrittenTokens >= estimatedTokens {
				if llm.RequestPayloadTooLarge(rewrittenBytes) {
					return preflightOversizeTurnResult(emitter, rewrittenBytes, rewrittenTokens, contextWindowTokens), nil
				}
				break
			}
			currentRequest = rewritten
			preflightRecovered = true
			if preflightRound >= 8 {
				if llm.RequestPayloadTooLarge(rewrittenBytes) {
					return preflightOversizeTurnResult(emitter, rewrittenBytes, rewrittenTokens, contextWindowTokens), nil
				}
				break
			}
		}

		turn, err := l.runSingleTurn(ctx, runCtx, currentRequest, emitter, yield, turnIndex)
		if err != nil {
			return turnResult{}, err
		}
		if isOversizeTurn(turn) {
			contextWindowTokens := 0
			if runCtx.PipelineRC != nil {
				contextWindowTokens = pipeline.ResolveRunContextWindowTokens(runCtx.PipelineRC)
			}
			if providerRecoveryUsed {
				return turn, nil
			}
			rewritten, recovered, currentInputErr, err := maybeRecoverOversizeRequest(
				ctx,
				runCtx,
				currentRequest,
				&turn,
				emitter,
				yield,
				turnIndex,
				"provider",
				sizeEstimator.Estimate,
			)
			if err != nil {
				return turnResult{}, err
			}
			if currentInputErr != nil {
				return currentInputOversizeTurnResult(emitter, currentInputErr), nil
			}
			if recovered {
				rewrittenBytes, estimateErr := sizeEstimator.Estimate(rewritten)
				if estimateErr != nil {
					return requestEstimatorFailureTurnResult(emitter, estimateErr), nil
				}
				rewrittenTokens := estimateTurnRequestLimitTokens(runCtx, rewritten)
				if llm.RequestExceedsLimits(rewrittenBytes, rewrittenTokens, contextWindowTokens) {
					return preflightOversizeTurnResult(emitter, rewrittenBytes, rewrittenTokens, contextWindowTokens), nil
				}
				currentRequest = rewritten
				providerRecoveryUsed = true
				continue
			}
			if preflightRecovered {
				payloadBytes, err := sizeEstimator.Estimate(currentRequest)
				if err != nil {
					return requestEstimatorFailureTurnResult(emitter, err), nil
				}
				contextWindowTokens := 0
				if runCtx.PipelineRC != nil {
					contextWindowTokens = pipeline.ResolveRunContextWindowTokens(runCtx.PipelineRC)
				}
				return preflightOversizeTurnResult(
					emitter,
					payloadBytes,
					estimateTurnRequestLimitTokens(runCtx, currentRequest),
					contextWindowTokens,
				), nil
			}
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
		if msg, _ := last.DataJSON["message"].(string); msg != "" {
			retryData["message"] = msg
		}
		if details, ok := last.DataJSON["details"]; ok && details != nil {
			retryData["details"] = details
		}

		if err := yield(emitter.Emit("run.llm.retry", retryData, nil, nil)); err != nil {
			return turnResult{}, err
		}

		select {
		case <-time.After(time.Duration(delayMs) * time.Millisecond):
		case <-ctx.Done():
			return turnResult{Cancelled: true}, nil
		}
		attempt++
	}

	return turnResult{}, fmt.Errorf("retry loop exited unexpectedly")
}

func maybeRecoverOversizeRequest(
	ctx context.Context,
	runCtx RunContext,
	request llm.Request,
	turn *turnResult,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
	turnIndex int,
	triggerPhase string,
	requestEstimate func(llm.Request) (int, error),
) (llm.Request, bool, *pipeline.CurrentInputOversizeError, error) {
	if runCtx.PipelineRC == nil {
		return request, false, nil, nil
	}
	rewritten, stats, rewriteErr := pipeline.RewriteOversizeRequest(
		ctx,
		runCtx.PipelineRC,
		request,
		currentContextCompactAnchor(runCtx),
		requestEstimate,
	)
	currentInputErr, currentInputTooLarge := pipeline.IsCurrentInputOversizeError(rewriteErr)
	phase := "completed"
	stillOversize := false
	if rewriteErr == nil && stats.RewriteApplied {
		stillOversize = llm.RequestExceedsLimits(
			stats.RequestBytesAfterRewrite,
			pipeline.EstimateRequestContextTokens(runCtx.PipelineRC, rewritten),
			pipeline.ResolveRunContextWindowTokens(runCtx.PipelineRC),
		)
	}
	switch {
	case currentInputTooLarge:
		phase = "current_input_too_large"
	case rewriteErr != nil:
		phase = "llm_failed"
	case stillOversize:
		phase = "still_oversize"
	case !stats.RewriteApplied:
		phase = "no_rewrite"
	}
	ev := emitter.Emit("run.context_compact", map[string]any{
		"op":                           "rewrite",
		"mode":                         "canonical_chunks",
		"phase":                        phase,
		"turn_index":                   turnIndex,
		"trigger_phase":                triggerPhase,
		"rewrite_applied":              stats.RewriteApplied,
		"images_stripped":              stats.ImagesStripped,
		"tool_results_microcompacted":  stats.ToolResultsMicrocompacted,
		"compact_applied":              stats.CompactApplied,
		"target_chunk_count":           stats.TargetChunkCount,
		"previous_replacement_count":   stats.PreviousReplacementCount,
		"single_atom_partial":          stats.SingleAtomPartial,
		"request_bytes_before_rewrite": stats.RequestBytesBeforeRewrite,
		"request_bytes_after_rewrite":  stats.RequestBytesAfterRewrite,
		"minimal_request_bytes":        stats.MinimalRequestBytes,
		"current_input_too_large":      stats.CurrentInputTooLarge,
		"request_still_oversize":       stillOversize,
	}, nil, nil)
	if turn != nil && len(turn.Events) > 0 {
		last := turn.Events[len(turn.Events)-1]
		if details, ok := last.DataJSON["details"].(map[string]any); ok {
			if statusCode, ok := anyToInt64(details["status_code"]); ok {
				ev.DataJSON["status_code"] = statusCode
			}
			if oversizePhase, ok := details["oversize_phase"].(string); ok && strings.TrimSpace(oversizePhase) != "" {
				ev.DataJSON["oversize_phase"] = strings.TrimSpace(oversizePhase)
			}
		}
	}
	if rewriteErr != nil {
		ev.DataJSON["llm_error"] = rewriteErr.Error()
	}
	if err := yield(ev); err != nil {
		return request, false, nil, err
	}
	if currentInputTooLarge {
		return request, false, currentInputErr, nil
	}
	if rewriteErr != nil {
		return request, false, nil, rewriteErr
	}
	return rewritten, stats.RewriteApplied, nil, nil
}

func preflightOversizeTurnResult(emitter events.Emitter, payloadBytes int, estimatedTokens int, contextWindowTokens int) turnResult {
	details := llm.OversizeFailureDetails(payloadBytes, llm.OversizePhasePreflight, map[string]any{
		"network_attempted": false,
	})
	if estimatedTokens > 0 {
		details["estimated_tokens"] = estimatedTokens
	}
	if contextWindowTokens > 0 {
		details["context_window_tokens"] = contextWindowTokens
	}
	ev := emitter.Emit("run.failed", llm.GatewayError{
		ErrorClass: llm.ErrorClassProviderNonRetryable,
		Message:    "request exceeds limits",
		Details:    details,
	}.ToJSON(), nil, stringPtr(llm.ErrorClassProviderNonRetryable))
	return turnResult{
		Events:   []events.RunEvent{ev},
		Terminal: true,
	}
}

func currentInputOversizeTurnResult(emitter events.Emitter, inputErr *pipeline.CurrentInputOversizeError) turnResult {
	currentEstimate := 0
	minimalEstimate := 0
	if inputErr != nil {
		currentEstimate = inputErr.CurrentRequestEstimate
		minimalEstimate = inputErr.MinimalRequestEstimate
	}
	details := llm.OversizeFailureDetails(currentEstimate, llm.OversizePhasePreflight, map[string]any{
		"network_attempted":      false,
		"current_input_oversize": true,
		"minimal_payload_bytes":  minimalEstimate,
	})
	if inputErr != nil {
		if inputErr.CurrentRequestTokens > 0 {
			details["estimated_tokens"] = inputErr.CurrentRequestTokens
		}
		if inputErr.MinimalRequestTokens > 0 {
			details["minimal_estimated_tokens"] = inputErr.MinimalRequestTokens
		}
		if inputErr.ContextWindowTokens > 0 {
			details["context_window_tokens"] = inputErr.ContextWindowTokens
		}
	}
	ev := emitter.Emit("run.failed", llm.GatewayError{
		ErrorClass: llm.ErrorClassProviderNonRetryable,
		Message:    "current input node exceeds request limit",
		Details:    details,
	}.ToJSON(), nil, stringPtr(llm.ErrorClassProviderNonRetryable))
	return turnResult{
		Events:   []events.RunEvent{ev},
		Terminal: true,
	}
}

func requestEstimatorFailureTurnResult(emitter events.Emitter, err error) turnResult {
	reason := "provider request estimator unavailable"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		reason = strings.TrimSpace(err.Error())
	}
	ev := emitter.Emit("run.failed", llm.GatewayError{
		ErrorClass: llm.ErrorClassInternalError,
		Message:    "provider request estimator unavailable",
		Details:    map[string]any{"reason": reason},
	}.ToJSON(), nil, stringPtr(llm.ErrorClassInternalError))
	return turnResult{
		Events:   []events.RunEvent{ev},
		Terminal: true,
	}
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

func isOversizeTurn(turn turnResult) bool {
	if len(turn.Events) == 0 {
		return false
	}
	last := turn.Events[len(turn.Events)-1]
	if last.Type != "run.failed" || last.ErrorClass == nil || *last.ErrorClass != llm.ErrorClassProviderNonRetryable {
		return false
	}
	details, _ := last.DataJSON["details"].(map[string]any)
	statusCode, ok := anyToInt64(details["status_code"])
	return ok && statusCode == 413
}

func currentContextCompactAnchor(runCtx RunContext) *pipeline.ContextCompactPressureAnchor {
	if runCtx.PipelineRC == nil || !runCtx.PipelineRC.HasContextCompactAnchor {
		return nil
	}
	return &pipeline.ContextCompactPressureAnchor{
		LastRealPromptTokens:             runCtx.PipelineRC.LastRealPromptTokens,
		LastRequestContextEstimateTokens: runCtx.PipelineRC.LastRequestContextEstimateTokens,
	}
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

	appendAssistantRollout := func() {
		if runCtx.RolloutRecorder == nil || !turnHasRecoverableProgressData(strings.Join(assistantChunks, ""), assistantMessage, toolCalls, toolResults) {
			return
		}
		var contentPartsJSON json.RawMessage
		if assistant := assistantMessageOrFallback(assistantMessage, assistantChunks); len(assistant.Content) > 0 {
			contentParts := make([]map[string]any, 0, len(assistant.Content))
			for _, part := range assistant.Content {
				contentParts = append(contentParts, part.ToJSON())
			}
			var marshalErr error
			contentPartsJSON, marshalErr = json.Marshal(contentParts)
			if marshalErr != nil {
				slog.WarnContext(ctx, "rollout: failed to marshal assistant content parts", "err", marshalErr)
			}
		}
		var tcJSON json.RawMessage
		if len(toolCalls) > 0 {
			var marshalErr error
			tcJSON, marshalErr = json.Marshal(toolCalls)
			if marshalErr != nil {
				slog.WarnContext(ctx, "rollout: failed to marshal assistant tool_calls", "err", marshalErr)
			}
		}
		appendRollout(ctx, runCtx.RolloutRecorder, MakeAssistantMessage(
			llm.VisibleMessageText(assistantMessageOrFallback(assistantMessage, assistantChunks)),
			contentPartsJSON,
			tcJSON,
		))
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
			appendAssistantRollout()
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
				appendAssistantRollout()
				appendRollout(ctx, runCtx.RolloutRecorder, MakeTurnEnd(turnIndex))
			}
			return turnResult{Events: eventsOut, Terminal: true}, nil
		}
	}

	if completed == nil && turnHasRecoverableProgressData(strings.Join(assistantChunks, ""), assistantMessage, toolCalls, toolResults) {
		retryable := llm.RetryableStreamEndedError()
		eventsOut = append(eventsOut, emitter.Emit("run.failed", retryable.ToJSON(), nil, stringPtr(retryable.ErrorClass)))
		if runCtx.RolloutRecorder != nil {
			appendAssistantRollout()
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
		Messages:        cloneMessages(messages),
		Temperature:     request.Temperature,
		MaxOutputTokens: request.MaxOutputTokens,
		Tools:           cloneToolSpecs(request.Tools),
		ToolChoice:      request.ToolChoice,
		PromptPlan:      clonePromptPlan(request.PromptPlan),
		Metadata:        copyMap(request.Metadata),
		ReasoningMode:   request.ReasoningMode,
	}
}

func refreshCacheSafeSnapshot(runCtx *RunContext, baseMessages []llm.Message, request llm.Request) {
	if runCtx == nil {
		return
	}
	runCtx.CacheSafeSnapshot = &CacheSafeSnapshot{
		PersonaID:       loopPersonaID(*runCtx),
		BaseMessages:    cloneMessages(baseMessages),
		Model:           request.Model,
		Messages:        cloneMessages(request.Messages),
		Tools:           cloneToolSpecs(request.Tools),
		MaxOutputTokens: cloneIntPtr(request.MaxOutputTokens),
		Temperature:     cloneFloatPtr(request.Temperature),
		ReasoningMode:   request.ReasoningMode,
		ToolChoice:      cloneToolChoice(request.ToolChoice),
		PromptPlan:      clonePromptPlan(request.PromptPlan),
	}
}

func promptCacheSnapshotFromLoopContext(runCtx RunContext) *subagentctl.PromptCacheSnapshot {
	if runCtx.CacheSafeSnapshot == nil {
		return nil
	}
	snapshot := runCtx.CacheSafeSnapshot
	return subagentctl.ClonePromptCacheSnapshot(&subagentctl.PromptCacheSnapshot{
		PersonaID:       strings.TrimSpace(snapshot.PersonaID),
		BaseMessages:    cloneMessages(snapshot.BaseMessages),
		Messages:        cloneMessages(snapshot.Messages),
		Tools:           cloneToolSpecs(snapshot.Tools),
		Model:           strings.TrimSpace(snapshot.Model),
		MaxOutputTokens: cloneIntPtr(snapshot.MaxOutputTokens),
		Temperature:     cloneFloatPtr(snapshot.Temperature),
		ReasoningMode:   strings.TrimSpace(snapshot.ReasoningMode),
		ToolChoice:      cloneToolChoice(snapshot.ToolChoice),
		PromptPlan:      clonePromptPlan(snapshot.PromptPlan),
	})
}

func loopPersonaID(runCtx RunContext) string {
	if runCtx.PipelineRC == nil || runCtx.PipelineRC.PersonaDefinition == nil {
		return ""
	}
	return strings.TrimSpace(runCtx.PipelineRC.PersonaDefinition.ID)
}

func prepareTurnRequestPromptCache(request *llm.Request, runCtx RunContext, state *promptCacheTurnState) {
	if request == nil || !promptCacheEnabled(runCtx) || len(request.Messages) == 0 {
		return
	}
	request.Tools = annotateToolCacheHints(request.Tools, runCtx)
	if request.PromptPlan == nil {
		request.PromptPlan = &llm.PromptPlan{}
	}
	lastIdx := len(request.Messages) - 1
	request.PromptPlan.MessageCache.Enabled = true
	request.PromptPlan.MessageCache.MarkerMessageIndex = lastIdx
	request.PromptPlan.MessageCache.ToolResultCacheCutIndex = lastIdx
	request.PromptPlan.MessageCache.ToolResultCacheReferences = true
	request.PromptPlan.MessageCache.PinnedCacheEdits = clonePromptCacheEditBlocks(state.PinnedCacheEdits)
	request.PromptPlan.MessageCache.NewCacheEdits = nil

	// Pin stable marker: first turn computes it, subsequent turns reuse the same position.
	if state.StableMarkerPinned && state.StableMarkerIndex >= 0 && state.StableMarkerIndex < lastIdx {
		request.PromptPlan.MessageCache.StableMarkerEnabled = true
		request.PromptPlan.MessageCache.StableMarkerMessageIndex = state.StableMarkerIndex
	} else if !state.StableMarkerPinned {
		if request.PromptPlan.MessageCache.StableMarkerEnabled {
			state.StableMarkerIndex = request.PromptPlan.MessageCache.StableMarkerMessageIndex
			state.StableMarkerPinned = true
		}
	}

	currentRefs := collectToolResultReferences(request.Messages, lastIdx)
	if len(state.KnownToolResultRefs) > 0 {
		deletions := missingToolResultReferences(state.KnownToolResultRefs, currentRefs)
		if len(deletions) > 0 {
			block := llm.PromptCacheEditsBlock{
				UserMessageIndex: lastUserMessageIndex(request.Messages),
				Edits:            make([]llm.PromptCacheEdit, 0, len(deletions)),
			}
			for _, ref := range deletions {
				block.Edits = append(block.Edits, llm.PromptCacheEdit{
					Type:           llm.CacheHintActionDelete,
					CacheReference: ref,
				})
			}
			request.PromptPlan.MessageCache.NewCacheEdits = &block
			state.PinnedCacheEdits = append(state.PinnedCacheEdits, block)
		}
	}
	state.KnownToolResultRefs = currentRefs
}

// computePlanMarkers 从 plan 计算 plan-time markers 概览，反映本轮请求"打算"如何放 cache marker。
// 严格基于 plan 数据，不依赖 anthropic 渲染结果。
func computePlanMarkers(request llm.Request) llm.PromptCachePlanMarkers {
	markers := llm.PromptCachePlanMarkers{
		MessageCacheControlIndex: -1,
		CacheReferenceToolUseIDs: []string{},
	}
	// system 段：plan.SystemBlocks 中 CacheEligible=true 的连续段会成为带 cache_control 的 block
	if request.PromptPlan != nil {
		var lastCacheType string
		var lastCacheScope string
		for _, block := range request.PromptPlan.SystemBlocks {
			text := strings.TrimSpace(block.Text)
			if text == "" {
				continue
			}
			cacheType := ""
			cacheScope := ""
			if block.CacheEligible {
				cacheType = "ephemeral"
				cacheScope = cacheScopeForStability(block.Stability)
			}
			// 模拟 anthropicSystemBlocksFromPlan 的合并逻辑：cacheType/cacheScope 切换时新起一段
			if cacheType != "" && (cacheType != lastCacheType || cacheScope != lastCacheScope) {
				markers.SystemBlocksWithCacheControl++
			}
			lastCacheType = cacheType
			lastCacheScope = cacheScope
		}

		mc := request.PromptPlan.MessageCache
		if mc.Enabled {
			markers.MessageCacheControlIndex = mc.MarkerMessageIndex
			if mc.StableMarkerEnabled && mc.StableMarkerMessageIndex >= 0 && mc.StableMarkerMessageIndex < mc.MarkerMessageIndex {
				markers.StableMarkerApplied = true
			}
			if mc.NewCacheEdits != nil {
				markers.CacheEditsCount += len(mc.NewCacheEdits.Edits)
			}
			for _, pinned := range mc.PinnedCacheEdits {
				markers.CacheEditsCount += len(pinned.Edits)
			}

			// cache_reference: 当 ToolResultCacheReferences=true 时，[0..ToolResultCacheCutIndex] 范围内的
			// tool 消息会被替换为 cache_reference
			if mc.ToolResultCacheReferences {
				cut := mc.ToolResultCacheCutIndex
				if cut < 0 || cut >= len(request.Messages) {
					cut = len(request.Messages) - 1
				}
				for i := 0; i <= cut && i < len(request.Messages); i++ {
					msg := request.Messages[i]
					if msg.Role != "tool" {
						continue
					}
					ref := extractToolCallID(msg)
					if ref == "" {
						continue
					}
					markers.CacheReferenceCount++
					markers.CacheReferenceToolUseIDs = append(markers.CacheReferenceToolUseIDs, ref)
				}
			}
		}
	}
	return markers
}

// cacheScopeForStability 返回 stability 对应的 cache scope 字符串，与 anthropic 实现保持一致。
func cacheScopeForStability(stability string) string {
	switch strings.ToLower(strings.TrimSpace(stability)) {
	case llm.CacheStabilitySessionPrefix:
		return "session"
	case llm.CacheStabilityVolatileTail:
		return "volatile"
	default:
		return ""
	}
}

// promptCacheBreakInfo 记录本轮与上一轮 cache prefix/tool 变化。用于 debug payload 的 break 子字段。
type promptCacheBreakInfo struct {
	ChangedBuckets  []string
	PrevStableHash  string
	CurrStableHash  string
	PrevStableBytes int
	CurrStableBytes int
}

// computePromptCacheBreak 计算本轮 break 信息并用当前状态更新 state 以便下一轮比较。
func computePromptCacheBreak(
	request llm.Request,
	state *promptCacheTurnState,
) (promptCacheBreakInfo, llm.RequestStats) {
	var info promptCacheBreakInfo
	stats := llm.ComputeRequestStats(request)
	if state == nil {
		return info, stats
	}
	if state.PreviousStableHash != "" {
		if state.PreviousStableHash != stats.StablePrefixHash {
			info.ChangedBuckets = append(info.ChangedBuckets, "stable_prefix")
		}
		if state.PreviousSessionHash != stats.SessionPrefixHash {
			info.ChangedBuckets = append(info.ChangedBuckets, "session_prefix")
		}
		if state.PreviousVolatileHash != stats.VolatileTailHash {
			info.ChangedBuckets = append(info.ChangedBuckets, "volatile_tail")
		}
		if state.PreviousToolHash != stats.ToolSchemaHash {
			info.ChangedBuckets = append(info.ChangedBuckets, "tool_schema")
		}
	}
	info.PrevStableHash = state.PreviousStableHash
	info.CurrStableHash = stats.StablePrefixHash
	info.PrevStableBytes = state.PreviousStableBytes
	info.CurrStableBytes = stats.StablePrefixBytes

	// 覆盖为当前轮值，供下一轮比较
	state.PreviousStableHash = stats.StablePrefixHash
	state.PreviousSessionHash = stats.SessionPrefixHash
	state.PreviousVolatileHash = stats.VolatileTailHash
	state.PreviousToolHash = stats.ToolSchemaHash
	state.PreviousStableBytes = stats.StablePrefixBytes
	return info, stats
}

func promptCacheEnabled(runCtx RunContext) bool {
	return runCtx.PipelineRC != nil &&
		runCtx.PipelineRC.AgentConfig != nil &&
		strings.TrimSpace(runCtx.PipelineRC.AgentConfig.PromptCacheControl) == "system_prompt"
}

func annotateToolCacheHints(specs []llm.ToolSpec, runCtx RunContext) []llm.ToolSpec {
	if len(specs) == 0 || !promptCacheEnabled(runCtx) {
		return specs
	}
	coreTools := map[string]struct{}{}
	if runCtx.PipelineRC != nil && runCtx.PipelineRC.PersonaDefinition != nil {
		for _, name := range runCtx.PipelineRC.PersonaDefinition.CoreTools {
			coreTools[strings.TrimSpace(name)] = struct{}{}
		}
	}
	lastCacheableIndex := -1
	for i, spec := range specs {
		if spec.CacheHint != nil {
			continue
		}
		lastCacheableIndex = i
	}
	out := make([]llm.ToolSpec, len(specs))
	for i, spec := range specs {
		out[i] = spec
		if spec.CacheHint != nil {
			continue
		}
		if i != lastCacheableIndex {
			continue
		}
		scope := "global"
		if len(coreTools) > 0 {
			if _, ok := coreTools[strings.TrimSpace(spec.Name)]; !ok {
				scope = "org"
			}
		}
		out[i].CacheHint = &llm.CacheHint{
			Action: llm.CacheHintActionWrite,
			Scope:  scope,
		}
	}
	return out
}

func collectToolResultReferences(messages []llm.Message, maxIndex int) map[string]struct{} {
	refs := map[string]struct{}{}
	limit := len(messages)
	if maxIndex >= 0 && maxIndex+1 < limit {
		limit = maxIndex + 1
	}
	for i := 0; i < limit; i++ {
		msg := messages[i]
		if msg.Role != "tool" {
			continue
		}
		ref := extractToolCallID(msg)
		if ref == "" {
			continue
		}
		refs[ref] = struct{}{}
	}
	return refs
}

func extractToolCallID(msg llm.Message) string {
	var raw strings.Builder
	for _, part := range msg.Content {
		raw.WriteString(llm.PartPromptText(part))
	}
	text := strings.TrimSpace(raw.String())
	if text == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return ""
	}
	ref, _ := payload["tool_call_id"].(string)
	return strings.TrimSpace(ref)
}

func missingToolResultReferences(previous map[string]struct{}, current map[string]struct{}) []string {
	if len(previous) == 0 {
		return nil
	}
	out := make([]string, 0, len(previous))
	for ref := range previous {
		if _, ok := current[ref]; ok {
			continue
		}
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func lastUserMessageIndex(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return i
		}
	}
	return -1
}

func clonePromptPlan(src *llm.PromptPlan) *llm.PromptPlan {
	if src == nil {
		return nil
	}
	cloned := *src
	if len(src.SystemBlocks) > 0 {
		cloned.SystemBlocks = append([]llm.PromptPlanBlock(nil), src.SystemBlocks...)
	}
	if len(src.MessageBlocks) > 0 {
		cloned.MessageBlocks = append([]llm.PromptPlanBlock(nil), src.MessageBlocks...)
	}
	cloned.MessageCache.PinnedCacheEdits = clonePromptCacheEditBlocks(src.MessageCache.PinnedCacheEdits)
	if src.MessageCache.NewCacheEdits != nil {
		block := *src.MessageCache.NewCacheEdits
		if len(block.Edits) > 0 {
			block.Edits = append([]llm.PromptCacheEdit(nil), block.Edits...)
		}
		cloned.MessageCache.NewCacheEdits = &block
	}
	return &cloned
}

func clonePromptCacheEditBlocks(src []llm.PromptCacheEditsBlock) []llm.PromptCacheEditsBlock {
	if len(src) == 0 {
		return nil
	}
	out := make([]llm.PromptCacheEditsBlock, len(src))
	for i, block := range src {
		out[i] = block
		if len(block.Edits) > 0 {
			out[i].Edits = append([]llm.PromptCacheEdit(nil), block.Edits...)
		}
	}
	return out
}

func cloneMessages(src []llm.Message) []llm.Message {
	if len(src) == 0 {
		return nil
	}
	cloned := subagentctl.ClonePromptCacheSnapshot(&subagentctl.PromptCacheSnapshot{BaseMessages: src})
	return cloned.BaseMessages
}

func cloneToolSpecs(src []llm.ToolSpec) []llm.ToolSpec {
	if len(src) == 0 {
		return nil
	}
	cloned := subagentctl.ClonePromptCacheSnapshot(&subagentctl.PromptCacheSnapshot{Tools: src})
	return cloned.Tools
}

func cloneToolChoice(src *llm.ToolChoice) *llm.ToolChoice {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func cloneIntPtr(src *int) *int {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func cloneFloatPtr(src *float64) *float64 {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

type requestSizeEstimator struct {
	ProviderBased bool
	Available     bool
	Err           error
	Estimate      func(llm.Request) (int, error)
}

func newRequestSizeEstimator(runCtx RunContext) requestSizeEstimator {
	fallback := requestSizeEstimator{
		ProviderBased: false,
		Available:     true,
		Estimate: func(req llm.Request) (int, error) {
			return llm.EstimateRequestJSONBytes(req), nil
		},
	}
	if runCtx.PipelineRC == nil {
		return fallback
	}
	estimate := runCtx.PipelineRC.EstimateProviderRequestBytes
	if estimate == nil {
		return fallback
	}
	requestProbe := llm.Request{
		Model:    strings.TrimSpace(runCtx.Model),
		Messages: []llm.Message{{Role: "user", Content: []llm.TextPart{{Text: "probe"}}}},
	}
	if size, err := estimate(requestProbe); err != nil || size <= 0 {
		return fallback
	}
	return requestSizeEstimator{
		ProviderBased: true,
		Available:     true,
		Estimate: func(req llm.Request) (int, error) {
			size, err := estimate(req)
			if err != nil || size <= 0 {
				return llm.EstimateRequestJSONBytes(req), nil
			}
			return size, nil
		},
	}
}

func estimateTurnRequestContextTokens(runCtx RunContext, request llm.Request) int {
	if runCtx.PipelineRC != nil {
		if estimate := pipeline.EstimateRequestContextTokens(runCtx.PipelineRC, request); estimate > 0 {
			return estimate
		}
	}
	sizeEstimator := newRequestSizeEstimator(runCtx)
	estimatedBytes, err := sizeEstimator.Estimate(request)
	if err != nil || estimatedBytes <= 0 {
		estimatedBytes = llm.EstimateRequestJSONBytes(request)
	}
	estimatedTokens := estimatedBytes / 4
	if estimatedTokens < 1 {
		return 1
	}
	return estimatedTokens
}

func estimateTurnRequestLimitTokens(runCtx RunContext, request llm.Request) int {
	raw := 0
	if runCtx.PipelineRC != nil {
		raw = pipeline.EstimateRequestContextTokens(runCtx.PipelineRC, request)
	}
	if raw <= 0 {
		raw = estimateTurnRequestContextTokens(runCtx, request)
	}
	return raw
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
	if promptTokens := contextPressureAnchorPromptTokens(usage); promptTokens > 0 {
		data["last_real_prompt_tokens"] = promptTokens
	}
}

func contextPressureAnchorPromptTokens(usage map[string]any) int64 {
	if usage == nil {
		return 0
	}
	inputTokens, ok := anyToInt64(usage["input_tokens"])
	if !ok || inputTokens <= 0 {
		return 0
	}
	if cacheReadTokens, ok := anyToInt64(usage["cache_read_input_tokens"]); ok && cacheReadTokens > 0 {
		inputTokens += cacheReadTokens
	}
	return inputTokens
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
var maxToolResultHistoryChars = 80_000

// compactToolResults returns a copy of messages where the oldest tool result
// messages are replaced by minimal placeholders if the total tool result
// character count exceeds maxToolResultHistoryChars.
// The original messages slice is never modified.
func compactToolResults(messages []llm.Message) []llm.Message {
	return compactToolResultsWithState(messages, nil)
}

func compactToolResultsWithState(messages []llm.Message, state *toolResultReplacementState) []llm.Message {
	total := 0
	prepared := applyStoredToolResultReplacements(messages, state)
	for _, m := range prepared {
		if m.Role == "tool" {
			for _, p := range m.Content {
				total += len(p.Text)
			}
		}
	}
	if total <= maxToolResultHistoryChars {
		return prepared
	}

	out := make([]llm.Message, len(prepared))
	copy(out, prepared)

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
		out[i] = compactedToolMessage(out[i], state)
		excess -= msgSize
	}
	return out
}

func applyStoredToolResultReplacements(messages []llm.Message, state *toolResultReplacementState) []llm.Message {
	if state == nil || len(state.ByToolCallID) == 0 {
		return messages
	}
	out := make([]llm.Message, len(messages))
	copy(out, messages)
	for i := range out {
		if out[i].Role != "tool" {
			continue
		}
		callID := toolMessageCallID(out[i])
		if callID == "" {
			continue
		}
		stubText, ok := state.ByToolCallID[callID]
		if !ok || strings.TrimSpace(stubText) == "" {
			continue
		}
		out[i] = llm.Message{
			Role:    "tool",
			Content: compactedContentParts(out[i].Content, stubText),
		}
	}
	return out
}

// compactedToolMessage returns a minimal version of a tool result message,
// preserving only the tool_call_id so the conversation structure stays valid.
func compactedToolMessage(m llm.Message, state *toolResultReplacementState) llm.Message {
	if len(m.Content) == 0 {
		return m
	}
	callID := toolMessageCallID(m)
	if state != nil && callID != "" {
		if stubText, ok := state.ByToolCallID[callID]; ok && strings.TrimSpace(stubText) != "" {
			return llm.Message{
				Role:    "tool",
				Content: compactedContentParts(m.Content, stubText),
			}
		}
	}
	stubText := buildCompactedToolStubText(m)
	if state != nil && callID != "" && strings.TrimSpace(stubText) != "" {
		state.ByToolCallID[callID] = stubText
	}
	return llm.Message{
		Role:    "tool",
		Content: compactedContentParts(m.Content, stubText),
	}
}

func buildCompactedToolStubText(m llm.Message) string {
	if len(m.Content) == 0 {
		return ""
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(m.Content[0].Text), &envelope); err != nil {
		callID := extractToolCallIDFromText(m.Content[0].Text)
		stub := map[string]any{
			"tool_call_id": callID,
			"result":       map[string]any{"compacted": true},
		}
		text, _ := json.Marshal(stub)
		return string(text)
	}
	toolName, _ := envelope["tool_name"].(string)
	if tools.IsGenerativeUIBootstrapTool(toolName) {
		return m.Content[0].Text
	}
	stub := map[string]any{
		"tool_call_id": envelope["tool_call_id"],
		"result":       map[string]any{"compacted": true},
	}
	if strings.TrimSpace(toolName) != "" {
		stub["tool_name"] = strings.TrimSpace(toolName)
	}
	text, _ := json.Marshal(stub)
	return string(text)
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

func toolMessageCallID(m llm.Message) string {
	if len(m.Content) == 0 {
		return ""
	}
	return extractToolCallIDFromText(m.Content[0].Text)
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
	return outcome != nil
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
