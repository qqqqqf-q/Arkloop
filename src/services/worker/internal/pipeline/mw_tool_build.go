package pipeline

import (
	"context"
	"log/slog"
	"strings"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	searchtools "arkloop/services/worker/internal/tools/builtin/search_tools"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var runtimeManagedToolNames = map[string]struct{}{
	"acp_agent":           {},
	"browser":             {},
	"conversation_search": {},
	"document_write":      {},
	"exec_command":        {},
	"memory_forget":       {},
	"memory_read":         {},
	"memory_search":       {},
	"memory_write":        {},
	"python_execute":      {},
	"web_fetch":           {},
	"web_search":          {},
	"write_stdin":         {},
}

// NewToolBuildMiddleware 根据最终的 allowlist 构建 DispatchingExecutor 和过滤后的 ToolSpecs。
// 当 persona 定义了 core_tools 时，将工具分为 core（直接可见）和 searchable（需要 search_tools 激活）两层。
func NewToolBuildMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		effectiveAllowlist := CopyStringSet(rc.AllowlistSet)

		resolvedAllowlist, err := ResolveProviderAllowlist(effectiveAllowlist, rc.ToolRegistry, rc.ActiveToolProviderByGroup)
		if err != nil {
			return err
		}
		resolvedAllowlist = filterAllowlistByRuntime(resolvedAllowlist, rc.Runtime)

		// When core_tools is configured, search_tools must be available regardless
		// of whether it was in the original allowlist (DB persona might not include it).
		hasCoreTools := rc.PersonaDefinition != nil && len(rc.PersonaDefinition.CoreTools) > 0
		if hasCoreTools {
			resolvedAllowlist["search_tools"] = struct{}{}
			if _, ok := rc.ToolRegistry.Get("search_tools"); !ok {
				_ = rc.ToolRegistry.Register(searchtools.AgentSpec)
			}
		}

		// Pre-bind search_tools executor before filtering so it survives
		// FilterAllowlistToBoundExecutors. Uses lazy reference because
		// DispatchingExecutor is created after this point.
		var dispatchRef *tools.DispatchingExecutor
		if _, inAllowlist := resolvedAllowlist["search_tools"]; inAllowlist {
			rc.ToolExecutors["search_tools"] = searchtools.NewExecutor(
				&lazyActivator{ref: &dispatchRef},
				func() map[string]llm.ToolSpec {
					if dispatchRef == nil {
						return nil
					}
					return dispatchRef.SearchableSpecs()
				},
			)
		}

		filteredAllowlist, dropped := FilterAllowlistToBoundExecutors(resolvedAllowlist, rc.ToolExecutors)
		if len(dropped) > 0 {
			slog.WarnContext(ctx, "tool allowlist dropped unbound executors", "run_id", rc.Run.ID, "tools", dropped)
		}

		filteredAllowlist = filterNotConfiguredExecutors(filteredAllowlist, rc.ToolExecutors)

		executor, err := BuildDispatchExecutor(rc.ToolRegistry, rc.ToolExecutors, filteredAllowlist)
		if err != nil {
			return err
		}
		dispatchRef = executor

		allSpecs := FilterToolSpecs(rc.ToolSpecs, filteredAllowlist, rc.ToolRegistry)

		// Ensure search_tools LLM spec is present when core_tools is active.
		// It might be missing if the persona's tool_allowlist narrowed ToolSpecs earlier.
		if hasCoreTools {
			hasSearchSpec := false
			for _, s := range allSpecs {
				if s.Name == "search_tools" {
					hasSearchSpec = true
					break
				}
			}
			if !hasSearchSpec {
				allSpecs = append(allSpecs, searchtools.LlmSpec)
			}
		}

		coreSet := resolveCoreToolSet(rc)
		if coreSet != nil {
			coreSpecs, searchableSpecs := splitToolSpecs(allSpecs, coreSet)
			if len(searchableSpecs) > 0 {
				searchableMap := make(map[string]llm.ToolSpec, len(searchableSpecs))
				for _, spec := range searchableSpecs {
					searchableMap[spec.Name] = spec
				}
				executor.SetSearchableSpecs(searchableMap)

				catalog := searchtools.BuildCatalogPrompt(searchableMap)
				if catalog != "" {
					rc.SystemPrompt = strings.TrimRight(rc.SystemPrompt, "\n") + "\n" + catalog
				}
			}
			allSpecs = coreSpecs
		}

		rc.ToolExecutor = executor
		rc.FinalSpecs = allSpecs

		return next(ctx, rc)
	}
}

// resolveCoreToolSet returns the set of core tool names from persona config.
// Returns nil when all tools should be core (backward compatible).
func resolveCoreToolSet(rc *RunContext) map[string]struct{} {
	if rc.PersonaDefinition == nil || len(rc.PersonaDefinition.CoreTools) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(rc.PersonaDefinition.CoreTools)+1)
	for _, name := range rc.PersonaDefinition.CoreTools {
		set[name] = struct{}{}
	}
	// search_tools is always core when core_tools is configured
	set["search_tools"] = struct{}{}
	return set
}

// splitToolSpecs partitions specs into core (in coreSet) and searchable (not in coreSet).
func splitToolSpecs(specs []llm.ToolSpec, coreSet map[string]struct{}) (core, searchable []llm.ToolSpec) {
	for _, spec := range specs {
		if _, ok := coreSet[spec.Name]; ok {
			core = append(core, spec)
		} else {
			searchable = append(searchable, spec)
		}
	}
	return
}

// lazyActivator wraps a pointer-to-pointer to DispatchingExecutor,
// allowing search_tools executor to be created before the DispatchingExecutor exists.
type lazyActivator struct {
	ref **tools.DispatchingExecutor
}

func (la *lazyActivator) Activate(specs ...llm.ToolSpec) {
	if la.ref != nil && *la.ref != nil {
		(*la.ref).Activate(specs...)
	}
}

func (la *lazyActivator) DrainActivated() []llm.ToolSpec {
	if la.ref != nil && *la.ref != nil {
		return (*la.ref).DrainActivated()
	}
	return nil
}

func filterNotConfiguredExecutors(allowlistSet map[string]struct{}, executors map[string]tools.Executor) map[string]struct{} {
	out := CopyStringSet(allowlistSet)
	for name := range out {
		if exec, ok := executors[name]; ok {
			if nc, ok := exec.(tools.NotConfiguredChecker); ok && nc.IsNotConfigured() {
				delete(out, name)
			}
		}
	}
	return out
}

func filterAllowlistByRuntime(allowlistSet map[string]struct{}, snapshot *sharedtoolruntime.RuntimeSnapshot) map[string]struct{} {
	out := CopyStringSet(allowlistSet)
	if snapshot == nil {
		return out
	}
	for name := range out {
		if _, managed := runtimeManagedToolNames[name]; !managed {
			continue
		}
		if snapshot.BuiltinAvailable(name) {
			continue
		}
		delete(out, name)
	}
	return out
}
