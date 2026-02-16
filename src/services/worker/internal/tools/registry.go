package tools

import (
	"fmt"
	"sort"
)

type Registry struct {
	specByName map[string]AgentToolSpec
}

func NewRegistry() *Registry {
	return &Registry{specByName: map[string]AgentToolSpec{}}
}

func (r *Registry) Register(spec AgentToolSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if _, exists := r.specByName[spec.Name]; exists {
		return fmt.Errorf("Tool 已注册：%s", spec.Name)
	}
	r.specByName[spec.Name] = spec
	return nil
}

func (r *Registry) Get(toolName string) (AgentToolSpec, bool) {
	spec, ok := r.specByName[toolName]
	return spec, ok
}

func (r *Registry) ListNames() []string {
	names := make([]string, 0, len(r.specByName))
	for name := range r.specByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

