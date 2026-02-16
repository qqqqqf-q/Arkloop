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
}

type Definition struct {
	ID           string
	Version      string
	Title        string
	Description  *string
	ToolAllowlist []string
	Budgets      Budgets
	PromptMD     string
}

type Registry struct {
	byID map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{byID: map[string]Definition{}}
}

func (r *Registry) Register(def Definition) error {
	if def.ID == "" {
		return fmt.Errorf("skill.id 不能为空")
	}
	if _, exists := r.byID[def.ID]; exists {
		return fmt.Errorf("skill.id 重复: %s", def.ID)
	}
	r.byID[def.ID] = def
	return nil
}

func (r *Registry) Get(skillID string) (Definition, bool) {
	def, ok := r.byID[skillID]
	return def, ok
}

func (r *Registry) ListIDs() []string {
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
