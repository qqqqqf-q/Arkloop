package executor

import "fmt"

// Registry 管理 AgentExecutor 工厂函数，按 executor_type 键控。
type Registry struct {
	factories map[string]Factory
}

// NewAgentRegistry 创建空注册表。
func NewAgentRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

// Register 注册 executor 类型对应的工厂函数。重复注册返回错误。
func (r *Registry) Register(executorType string, factory Factory) error {
	if executorType == "" {
		return fmt.Errorf("executor type must not be empty")
	}
	if factory == nil {
		return fmt.Errorf("factory must not be nil")
	}
	if _, exists := r.factories[executorType]; exists {
		return fmt.Errorf("executor type already registered: %s", executorType)
	}
	r.factories[executorType] = factory
	return nil
}

// Build 根据类型和配置构建 AgentExecutor 实例。未知类型返回描述性错误。
func (r *Registry) Build(executorType string, config map[string]any) (AgentExecutor, error) {
	factory, ok := r.factories[executorType]
	if !ok {
		return nil, fmt.Errorf("unknown executor type: %s", executorType)
	}
	if config == nil {
		config = map[string]any{}
	}
	return factory(config)
}
