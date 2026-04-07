package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/agent"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"github.com/google/uuid"
)

const (
	defaultCheckInEvery   = 5
	defaultMaxWaitSeconds = 300
)

// InteractiveExecutor 在每 checkInEvery 轮迭代后暂停，等待用户注入消息后继续。
// 供 Ultra 等需要 Human-in-the-loop 的 Agent 使用；Lite/Pro 使用 SimpleExecutor，零开销。
type InteractiveExecutor struct {
	checkInEvery   int
	maxWaitSeconds int
}

// NewInteractiveExecutor 是 "agent.interactive" 的工厂函数。
// executor_config 支持：
//   - check_in_every  int（默认 5）：每隔几轮触发一次 check-in
//   - max_wait_seconds int（默认 300）：等待用户输入的超时秒数
func NewInteractiveExecutor(config map[string]any) (pipeline.AgentExecutor, error) {
	checkInEvery := defaultCheckInEvery
	if raw, ok := config["check_in_every"]; ok {
		n, err := toPositiveInt(raw, "check_in_every")
		if err != nil {
			return nil, err
		}
		checkInEvery = n
	}

	maxWaitSeconds := defaultMaxWaitSeconds
	if raw, ok := config["max_wait_seconds"]; ok {
		n, err := toPositiveInt(raw, "max_wait_seconds")
		if err != nil {
			return nil, err
		}
		maxWaitSeconds = n
	}

	return &InteractiveExecutor{
		checkInEvery:   checkInEvery,
		maxWaitSeconds: maxWaitSeconds,
	}, nil
}

func (e *InteractiveExecutor) Execute(
	ctx context.Context,
	rc *pipeline.RunContext,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) error {
	// 设置 rc.CheckInAt，供外部观察者（如日志/监控）读取
	rc.CheckInAt = func(iter int) bool {
		return iter%e.checkInEvery == 0
	}

	messages := append([]llm.Message{}, rc.Messages...)
	if strings.TrimSpace(rc.SystemPrompt) != "" {
		systemPart := llm.TextPart{Text: rc.SystemPrompt}
		if rc.AgentConfig != nil && rc.AgentConfig.PromptCacheControl == "system_prompt" {
			ephemeral := "ephemeral"
			systemPart.CacheControl = &ephemeral
		}
		messages = append([]llm.Message{
			{
				Role:    "system",
				Content: []llm.TextPart{systemPart},
			},
		}, messages...)
	}
	messages = applyImageFilter(rc.SelectedRoute, messages, rc.ReadCapabilities.ReadImageSourcesVisible)

	agentRequest := llm.Request{
		Model:           rc.SelectedRoute.Route.Model,
		Messages:        messages,
		Tools:           append([]llm.ToolSpec{}, rc.FinalSpecs...),
		MaxOutputTokens: rc.MaxOutputTokens,
		Temperature:     rc.Temperature,
		ReasoningMode:   rc.ReasoningMode,
		ToolChoice:      rc.ToolChoice,
	}

	maxWait := time.Duration(e.maxWaitSeconds) * time.Second

	// currentSegID 追踪当前活跃的 segment，供 PreIterHook/IterHook 共享。
	var currentSegID string

	runCtx := agent.RunContext{
		RunID:                            rc.Run.ID,
		AccountID:                        &rc.Run.AccountID,
		UserID:                           rc.UserID,
		AgentID:                          agentIDFromPersona(rc),
		ThreadID:                         &rc.Run.ThreadID,
		ProjectID:                        rc.Run.ProjectID,
		ProfileRef:                       rc.ProfileRef,
		WorkspaceRef:                     rc.WorkspaceRef,
		WorkDir:                          rc.WorkDir,
		EnabledSkills:                    append([]skillstore.ResolvedSkill(nil), rc.EnabledSkills...),
		ToolAllowlist:                    sortedToolNames(rc.AllowlistSet),
		ToolDenylist:                     append([]string(nil), rc.ToolDenylist...),
		ActiveToolProviderConfigsByGroup: copyProviderConfigMap(rc.ActiveToolProviderConfigsByGroup),
		RouteID:                          rc.SelectedRoute.Route.ID,
		Model:                            rc.SelectedRoute.Route.Model,
		MemoryScope:                      "same_user",
		TraceID:                          rc.TraceID,
		Tracer:                           rc.Tracer,
		InputJSON:                        rc.InputJSON,
		ReasoningIterations:              rc.ReasoningIterations,
		ToolContinuationBudget:           rc.ToolContinuationBudget,
		MaxParallelToolCalls:             rc.MaxParallelTasks,
		ToolExecutor:                     rc.ToolExecutor,
		ToolTimeoutMs:                    rc.ToolTimeoutMs,
		ToolBudget:                       rc.ToolBudget,
		PerToolSoftLimits:                rc.PerToolSoftLimits,
		MaxCostMicros:                    rc.MaxCostMicros,
		MaxTotalOutputTokens:             rc.MaxTotalOutputTokens,
		PendingMemoryWrites:              rc.PendingMemoryWrites,
		Runtime:                          rc.Runtime,
		LlmRetryMaxAttempts:              rc.LlmRetryMaxAttempts,
		LlmRetryBaseDelayMs:              rc.LlmRetryBaseDelayMs,
		WaitForInput:                     rc.WaitForInput,
		PollSteeringInput:                rc.PollSteeringInput,
		UserPromptScanFunc:               rc.UserPromptScanFunc,
		ToolOutputScanFunc:               rc.ToolOutputScanFunc,
		Channel:                          rc.ChannelToolSurface,
		CancelSignal: func() bool {
			return ctx.Err() != nil
		},
		RunDeadline:           rc.RunWallClockTimeout,
		PausedInputTimeout:    rc.PausedInputTimeout,
		IdleHeartbeatInterval: rc.IdleHeartbeatInterval,
		StreamThinking:        rc.StreamThinking,
		PipelineRC:            rc,
		PreIterHook: func(_ context.Context, iter int) error {
			// 关闭上一轮 segment（第 2 轮开始时关闭第 1 轮）
			if currentSegID != "" {
				ev := emitter.Emit("run.segment.end", map[string]any{
					"segment_id": currentSegID,
				}, nil, nil)
				if err := yield(ev); err != nil {
					return err
				}
			}
			// 开启新一轮 segment
			segID := uuid.NewString()
			currentSegID = segID
			ev := emitter.Emit("run.segment.start", map[string]any{
				"segment_id": segID,
				"kind":       "planning_round",
				"display": map[string]any{
					"mode":  "collapsed",
					"label": fmt.Sprintf("第 %d 轮规划", iter),
				},
			}, nil, nil)
			return yield(ev)
		},
		IterHook: func(hookCtx context.Context, reasoningIter int) (string, bool, error) {
			if reasoningIter%e.checkInEvery != 0 {
				return "", false, nil
			}
			if rc.WaitForInput == nil {
				return "", false, nil
			}

			ev := emitter.Emit(pipeline.EventTypeInputRequested, map[string]any{
				"iter": reasoningIter,
			}, nil, nil)
			if err := yield(ev); err != nil {
				return "", false, err
			}

			waitCtx, cancel := context.WithTimeout(hookCtx, maxWait)
			defer cancel()

			text, ok := rc.WaitForInput(waitCtx)
			return text, ok, nil
		},
	}

	loop := agent.NewLoop(rc.Gateway, rc.ToolExecutor)
	runErr := loop.Run(ctx, runCtx, agentRequest, emitter, yield)

	// 关闭最后一轮未关闭的 segment（最后一轮没有下一轮 PreIterHook 来关闭它）
	if currentSegID != "" {
		ev := emitter.Emit("run.segment.end", map[string]any{
			"segment_id": currentSegID,
		}, nil, nil)
		_ = yield(ev)
	}

	return runErr
}

// toPositiveInt 将 map 值转为正整数，用于解析 executor_config 字段。
func toPositiveInt(raw any, key string) (int, error) {
	switch v := raw.(type) {
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("executor_config.%s must be a positive integer", key)
		}
		return v, nil
	case float64:
		n := int(v)
		if n <= 0 {
			return 0, fmt.Errorf("executor_config.%s must be a positive integer", key)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("executor_config.%s must be an integer", key)
	}
}
