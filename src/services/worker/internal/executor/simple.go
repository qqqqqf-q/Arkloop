package executor

import (
	"context"

	"arkloop/services/shared/rollout"
	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/agent"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
)

// SimpleExecutor 封装标准 agent.Loop 执行路径，供 Lite/Pro 使用。
// 逻辑与 handler_agent_loop.go 中的 loop 执行部分完全一致。
type SimpleExecutor struct{}

// NewSimpleExecutor 是 "agent.simple" 的工厂函数，config 参数无效。
func NewSimpleExecutor(_ map[string]any) (pipeline.AgentExecutor, error) {
	return &SimpleExecutor{}, nil
}

func (e *SimpleExecutor) Execute(
	ctx context.Context,
	rc *pipeline.RunContext,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) error {
	planned := planRequestFromRunContext(rc, requestPlannerInput{
		Model:            rc.SelectedRoute.Route.Model,
		BaseMessages:     rc.Messages,
		PromptMode:       promptPlanModeFull,
		Tools:            rc.FinalSpecs,
		MaxOutputTokens:  rc.MaxOutputTokens,
		Temperature:      rc.Temperature,
		ReasoningMode:    rc.ReasoningMode,
		ToolChoice:       rc.ToolChoice,
		ApplyImageFilter: true,
	})
	agentRequest := planned.Request
	if rc.RolloutRecorder != nil && rc.ResumePromptSnapshot == nil {
		appendPromptSnapshot(ctx, rc, agentRequest)
	}

	runCtx := agent.RunContext{
		RunID:                            rc.Run.ID,
		AccountID:                        &rc.Run.AccountID,
		UserID:                           rc.UserID,
		AgentID:                          pipeline.StableAgentID(rc),
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
		CacheSafeSnapshot:     planned.CacheSafeSnapshot,
		RolloutRecorder:       rc.RolloutRecorder,
	}
	loop := agent.NewLoop(rc.Gateway, rc.ToolExecutor)
	return loop.Run(ctx, runCtx, agentRequest, emitter, yield)
}

func appendPromptSnapshot(ctx context.Context, rc *pipeline.RunContext, req llm.Request) {
	segments := rc.PromptSegments()
	snapshotSegments := make([]rollout.PromptSegment, 0, len(segments))
	for _, segment := range segments {
		snapshotSegments = append(snapshotSegments, rollout.PromptSegment{
			Name:          segment.Name,
			Target:        string(segment.Target),
			Role:          segment.Role,
			Text:          segment.Text,
			Stability:     string(segment.Stability),
			CacheEligible: segment.CacheEligible,
		})
	}
	snapshot := rollout.PromptSnapshot{
		Segments:      snapshotSegments,
		SystemPrompt:  rc.MaterializedSystemPrompt(),
		RuntimePrompt: rc.MaterializedRuntimePrompt(),
		RequestModel:  req.Model,
		ReasoningMode: rc.ReasoningMode,
		WorkDir:       rc.WorkDir,
		ProfileRef:    rc.ProfileRef,
		WorkspaceRef:  rc.WorkspaceRef,
	}
	if rc.SelectedRoute != nil {
		snapshot.SelectedRouteID = rc.SelectedRoute.Route.ID
		snapshot.SelectedModel = rc.SelectedRoute.Route.Model
	}
	if rc.PersonaDefinition != nil {
		snapshot.PersonaID = rc.PersonaDefinition.ID
	}
	_ = rc.RolloutRecorder.Append(ctx, agent.MakePromptSnapshot(snapshot))
}
