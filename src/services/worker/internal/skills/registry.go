package skills

import (
	"fmt"
	"sort"
)

type Budgets struct {
	MaxIterations   *int
	MaxOutputTokens *int
	ToolTimeoutMs   *int
	ToolBudget      map[string]any
	Temperature     *float64
	TopP            *float64
}

type Definition struct {
	ID               string
	Version          string
	Title            string
	Description      *string
	ToolAllowlist    []string
	ToolDenylist     []string
	Budgets          Budgets
	PromptMD         string
	ExecutorType     string         // 执行策略类型，默认 "agent.simple"
	ExecutorConfig   map[string]any // Executor 配置，默认 {}
	PreferredCredential *string     // 偏好凭证名称，nil 表示不绑定
	AgentConfigName  *string        // 显式绑定 AgentConfig 名称，nil 则走继承链
}

type Registry struct {
	byID map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{byID: map[string]Definition{}}
}

func (r *Registry) Register(def Definition) error {
	if def.ID == "" {
		return fmt.Errorf("skill.id must not be empty")
	}
	if _, exists := r.byID[def.ID]; exists {
		return fmt.Errorf("skill.id duplicate: %s", def.ID)
	}
	r.byID[def.ID] = def
	return nil
}

func (r *Registry) Get(skillID string) (Definition, bool) {
	def, ok := r.byID[skillID]
	return def, ok
}

// Set 覆盖写入，用于 DB skill 覆盖同 ID 的文件系统 skill。
func (r *Registry) Set(def Definition) {
	r.byID[def.ID] = def
}

func (r *Registry) ListIDs() []string {
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
