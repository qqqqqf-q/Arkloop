package pipeline

import (
	"context"

	"arkloop/services/worker/internal/events"
)

// AgentExecutor 定义 Agent 执行策略接口。
// pipeline 包定义接口，executor 包实现，避免循环导入。
type AgentExecutor interface {
	Execute(
		ctx context.Context,
		rc *RunContext,
		emitter events.Emitter,
		yield func(events.RunEvent) error,
	) error
}

// AgentExecutorBuilder 按类型和配置构建 AgentExecutor。
type AgentExecutorBuilder interface {
	Build(executorType string, config map[string]any) (AgentExecutor, error)
}
