package executor

import "arkloop/services/worker/internal/pipeline"

// Factory 根据 executor_config 构建 AgentExecutor 实例。
// AgentExecutor 接口定义在 pipeline 包，避免循环导入。
type Factory func(config map[string]any) (pipeline.AgentExecutor, error)
