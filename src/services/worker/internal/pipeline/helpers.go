package pipeline

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

func CopyToolExecutors(src map[string]tools.Executor) map[string]tools.Executor {
	out := make(map[string]tools.Executor, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func CopyStringSet(src map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(src))
	for k := range src {
		out[k] = struct{}{}
	}
	return out
}

// ForkRegistry 创建一个包含 base 所有 spec + 额外 spec 的新 Registry。
func ForkRegistry(base *tools.Registry, extras []tools.AgentToolSpec) *tools.Registry {
	r := tools.NewRegistry()
	for _, name := range base.ListNames() {
		spec, ok := base.Get(name)
		if ok {
			_ = r.Register(spec)
		}
	}
	for _, spec := range extras {
		if err := r.Register(spec); err != nil {
			slog.Warn("mcp tool name conflict, skipped", "name", spec.Name)
		}
	}
	return r
}

func BuildDispatchExecutor(
	registry *tools.Registry,
	executors map[string]tools.Executor,
	allowlistSet map[string]struct{},
) (*tools.DispatchingExecutor, error) {
	allowlistNames := make([]string, 0, len(allowlistSet))
	for name := range allowlistSet {
		allowlistNames = append(allowlistNames, name)
	}
	sort.Strings(allowlistNames)

	allowlist := tools.AllowlistFromNames(allowlistNames)
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	dispatch := tools.NewDispatchingExecutor(registry, policy)
	for _, toolName := range allowlistNames {
		bound := executors[toolName]
		if bound == nil {
			return nil, fmt.Errorf("tool executor not bound: %s", toolName)
		}
		if err := dispatch.Bind(toolName, bound); err != nil {
			return nil, err
		}
	}
	return dispatch, nil
}

// FilterAllowlistToBoundExecutors 丢弃没有绑定 executor 的工具，避免 allowlist 误配导致整个 run 失败。
func FilterAllowlistToBoundExecutors(allowlistSet map[string]struct{}, executors map[string]tools.Executor) (map[string]struct{}, []string) {
	filtered := map[string]struct{}{}
	if len(allowlistSet) == 0 {
		return filtered, nil
	}

	var dropped []string
	for name := range allowlistSet {
		if executors != nil && executors[name] != nil {
			filtered[name] = struct{}{}
			continue
		}
		dropped = append(dropped, name)
	}
	sort.Strings(dropped)
	return filtered, dropped
}

func FilterToolSpecs(specs []llm.ToolSpec, allowlistSet map[string]struct{}, registry *tools.Registry) []llm.ToolSpec {
	if len(allowlistSet) == 0 {
		return nil
	}
	if registry == nil {
		out := make([]llm.ToolSpec, 0, len(specs))
		for _, spec := range specs {
			if _, ok := allowlistSet[spec.Name]; !ok {
				continue
			}
			out = append(out, spec)
		}
		return out
	}

	allowedLlmNames := map[string]struct{}{}
	for name := range allowlistSet {
		llmName := strings.TrimSpace(name)
		if toolSpec, ok := registry.Get(name); ok {
			llmName = strings.TrimSpace(toolSpec.LlmName)
			if llmName == "" {
				llmName = toolSpec.Name
			}
		}
		if llmName == "" {
			continue
		}
		allowedLlmNames[llmName] = struct{}{}
	}

	out := make([]llm.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if _, ok := allowedLlmNames[spec.Name]; !ok {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func ResolveToolGroupName(registry *tools.Registry, toolName string) string {
	cleaned := strings.TrimSpace(toolName)
	if cleaned == "" {
		return ""
	}
	if registry != nil {
		if spec, ok := registry.Get(cleaned); ok {
			llmName := strings.TrimSpace(spec.LlmName)
			if llmName != "" {
				return llmName
			}
			return spec.Name
		}
	}
	return cleaned
}

func resolveProviderBindingGroup(registry *tools.Registry, toolName string) string {
	logicalName := ResolveToolGroupName(registry, toolName)
	if logicalName == "" {
		return ""
	}
	if meta, ok := sharedtoolmeta.Lookup(logicalName); ok {
		switch meta.Group {
		case sharedtoolmeta.GroupWebSearch, sharedtoolmeta.GroupWebFetch:
			return meta.Group
		}
	}
	return logicalName
}

// ToolAllowed 判断 toolName 是否在 allowlistSet 中可用：
// - toolName 本身在 set 中：允许
// - toolName 为 provider 且其 group（LlmName）在 set 中：允许
func ToolAllowed(allowlistSet map[string]struct{}, registry *tools.Registry, toolName string) bool {
	if allowlistSet == nil {
		return false
	}
	cleaned := strings.TrimSpace(toolName)
	if cleaned == "" {
		return false
	}
	if _, ok := allowlistSet[cleaned]; ok {
		return true
	}
	group := resolveProviderBindingGroup(registry, cleaned)
	if group == "" {
		return false
	}
	if _, ok := allowlistSet[group]; ok {
		return true
	}
	for name := range allowlistSet {
		if resolveProviderBindingGroup(registry, name) == group {
			return true
		}
	}
	return false
}

func RemoveToolOrGroup(allowlistSet map[string]struct{}, registry *tools.Registry, toolName string) {
	if allowlistSet == nil {
		return
	}
	cleaned := strings.TrimSpace(toolName)
	if cleaned == "" {
		return
	}
	delete(allowlistSet, cleaned)

	group := resolveProviderBindingGroup(registry, cleaned)
	if group == "" {
		return
	}

	for name := range CopyStringSet(allowlistSet) {
		if resolveProviderBindingGroup(registry, name) == group {
			delete(allowlistSet, name)
		}
	}

	if registry == nil {
		return
	}
	for _, name := range registry.ListNames() {
		if resolveProviderBindingGroup(registry, name) == group {
			delete(allowlistSet, name)
		}
	}
}

type toolGroupCandidateState struct {
	legacyName string
	providers  []string
}

// ResolveProviderAllowlist 把 allowlist（可能包含 group / provider 混合）解析为最终可执行的集合。
// 每个 group 最多只会选择一个 provider（或回落到 legacy group）。
func ResolveProviderAllowlist(
	effectiveAllowlist map[string]struct{},
	registry *tools.Registry,
	activeByGroup map[string]string,
) (map[string]struct{}, error) {
	resolved := map[string]struct{}{}
	if len(effectiveAllowlist) == 0 {
		return resolved, nil
	}

	groups := map[string]*toolGroupCandidateState{}
	for name := range effectiveAllowlist {
		group := resolveProviderBindingGroup(registry, name)
		if group == "" {
			continue
		}
		state := groups[group]
		if state == nil {
			state = &toolGroupCandidateState{}
			groups[group] = state
		}

		llmName := ""
		if registry != nil {
			if spec, ok := registry.Get(name); ok {
				llmName = strings.TrimSpace(spec.LlmName)
			}
		}

		if llmName != "" && name != llmName {
			state.providers = append(state.providers, name)
			continue
		}
		if registry != nil {
			if _, ok := registry.Get(name); ok {
				if state.legacyName == "" {
					state.legacyName = name
				}
				continue
			}
		}
		if state.legacyName == "" {
			state.legacyName = name
		}
	}

	groupNames := make([]string, 0, len(groups))
	for group := range groups {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)

	for _, group := range groupNames {
		state := groups[group]
		if state == nil {
			continue
		}

		if activeByGroup != nil {
			if active := strings.TrimSpace(activeByGroup[group]); active != "" {
				resolved[active] = struct{}{}
				continue
			}
		}

		if len(state.providers) == 1 {
			resolved[state.providers[0]] = struct{}{}
			continue
		}

		if meta, ok := sharedtoolmeta.Lookup(group); ok {
			switch meta.Group {
			case sharedtoolmeta.GroupWebSearch, sharedtoolmeta.GroupWebFetch:
				continue
			}
		}

		if strings.TrimSpace(state.legacyName) != "" {
			resolved[state.legacyName] = struct{}{}
			continue
		}

		if len(state.providers) == 0 {
			return nil, fmt.Errorf("tool group not resolved: %s", group)
		}
		sort.Strings(state.providers)
		return nil, fmt.Errorf("tool group ambiguous: %s (%s)", group, strings.Join(state.providers, ","))
	}

	return resolved, nil
}

func StringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
