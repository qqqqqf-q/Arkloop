package executor

import (
	"context"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/pipeline"
)

// AgentExecutor 定义 Agent 的执行策略。
// Lite/Pro/Ultra/Task 各自实现不同行为，通过注册表按 executor_type 分发。
type AgentExecutor interface {
	Execute(
		ctx context.Context,
		rc *pipeline.RunContext,
		emitter events.Emitter,
		yield func(events.RunEvent) error,
	) error
}

// Factory 根据 executor_config 构建 AgentExecutor 实例。
type Factory func(config map[string]any) (AgentExecutor, error)
