package executor

import (
	"context"
	"strings"

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

	agentRequest := llm.Request{
		Model:           rc.SelectedRoute.Route.Model,
		Messages:        messages,
		Tools:           append([]llm.ToolSpec{}, rc.FinalSpecs...),
		MaxOutputTokens: rc.MaxOutputTokens,
		Temperature:     rc.Temperature,
		ReasoningMode:   rc.ReasoningMode,
	}

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
		InputJSON:                        rc.InputJSON,
		ReasoningIterations:              rc.ReasoningIterations,
		ToolContinuationBudget:           rc.ToolContinuationBudget,
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
		UserPromptScanFunc:               rc.UserPromptScanFunc,
		ToolOutputScanFunc:               rc.ToolOutputScanFunc,
		Channel:                          rc.ChannelToolSurface,
		CancelSignal: func() bool {
			return ctx.Err() != nil
		},
		StreamThinking: rc.StreamThinking,
	}

	loop := agent.NewLoop(rc.Gateway, rc.ToolExecutor)
	return loop.Run(ctx, runCtx, agentRequest, emitter, yield)
}
