package pipeline

import (
	"context"
	"log/slog"
	"strings"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	loadtools "arkloop/services/worker/internal/tools/builtin/load_tools"
	"arkloop/services/worker/internal/tools/builtin/read"
)

var runtimeManagedToolNames = map[string]struct{}{
	"acp_agent":            {},
	"browser":              {},
	"conversation_search":  {},
	"create_artifact":      {},
	"document_write":       {},
	"exec_command":         {},
	"group_history_search": {},
	"memory_forget":        {},
	"memory_edit":          {},
	"memory_read":          {},
	"memory_search":        {},
	"memory_write":         {},
	"python_execute":       {},
	"web_fetch":            {},
	"web_search":           {},
	"write_stdin":          {},
}

// NewToolBuildMiddleware 根据最终的 allowlist 构建 DispatchingExecutor 和过滤后的 ToolSpecs。
// 当 persona 定义了 core_tools 时，将工具分为 core（直接可见）和 searchable（需要 load_tools 激活）两层。
func NewToolBuildMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		effectiveAllowlist := CopyStringSet(rc.AllowlistSet)

		resolvedAllowlist, err := ResolveProviderAllowlist(effectiveAllowlist, rc.ToolRegistry, rc.ActiveToolProviderByGroup)
		if err != nil {
			return err
		}
		// 主模型原生支持图片时，不需要走 bridge：通过 ContentParts 直返图片给主模型
		if supportsImageInput(rc.SelectedRoute) {
			if _, has := resolvedAllowlist[read.ProviderNameMiniMax]; has {
				delete(resolvedAllowlist, read.ProviderNameMiniMax)
				resolvedAllowlist[read.AgentSpec.Name] = struct{}{}
			}
		}
		resolvedAllowlist = filterAllowlistByRuntime(resolvedAllowlist, rc.Runtime, rc.ToolRegistry, rc.ActiveToolProviderByGroup)

		if rc.UserID == nil {
			resolvedAllowlist = filterIdentityRequiredTools(resolvedAllowlist)
		}

		// When core_tools is configured, load_tools and load_skill must be available regardless
		// of whether it was in the original allowlist (DB persona might not include it).
		hasCoreTools := rc.PersonaDefinition != nil && len(rc.PersonaDefinition.CoreTools) > 0
		if hasCoreTools {
			resolvedAllowlist["load_tools"] = struct{}{}
			resolvedAllowlist["load_skill"] = struct{}{}
			if _, ok := rc.ToolRegistry.Get("load_tools"); !ok {
				_ = rc.ToolRegistry.Register(loadtools.AgentSpec)
			}
		}

		// Pre-bind load_tools executor before filtering so it survives
		// FilterAllowlistToBoundExecutors. Uses lazy reference because
		// DispatchingExecutor is created after this point.
		// coreSpecsMap is populated after splitToolSpecs; the closure captures it by reference.
		var dispatchRef *tools.DispatchingExecutor
		var coreSpecsMap map[string]llm.ToolSpec
		if _, inAllowlist := resolvedAllowlist["load_tools"]; inAllowlist {
			rc.ToolExecutors["load_tools"] = loadtools.NewExecutor(
				&lazyActivator{ref: &dispatchRef},
				func() map[string]llm.ToolSpec {
					if dispatchRef == nil {
						return nil
					}
					return dispatchRef.SearchableSpecs()
				},
				func() map[string]llm.ToolSpec {
					return coreSpecsMap
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
		readImageBridgeEnabled := hasImageBridgeProvider(rc.ActiveToolProviderByGroup)
		nativeImageInput := supportsImageInput(rc.SelectedRoute)
		allSpecs = ApplyReadImageSourceVisibility(allSpecs, readImageBridgeEnabled, nativeImageInput)

		// Ensure load_tools LLM spec is present when core_tools is active.
		// It might be missing if the persona's tool_allowlist narrowed ToolSpecs earlier.
		if hasCoreTools {
			hasSearchSpec := false
			for _, s := range allSpecs {
				if s.Name == "load_tools" {
					hasSearchSpec = true
					break
				}
			}
			if !hasSearchSpec {
				allSpecs = append(allSpecs, loadtools.LlmSpec)
			}
		}

		coreSet := resolveCoreToolSet(rc)
		if coreSet != nil {
			coreSpecs, searchableSpecs := splitToolSpecs(allSpecs, coreSet)

			// Populate coreSpecsMap so load_tools can resolve queries against active tools.
			if len(coreSpecs) > 0 {
				coreSpecsMap = make(map[string]llm.ToolSpec, len(coreSpecs))
				for _, spec := range coreSpecs {
					coreSpecsMap[spec.Name] = spec
				}
			}

			if len(searchableSpecs) > 0 {
				searchableMap := make(map[string]llm.ToolSpec, len(searchableSpecs))
				for _, spec := range searchableSpecs {
					searchableMap[spec.Name] = spec
				}
				executor.SetSearchableSpecs(searchableMap)

				catalog := loadtools.BuildCatalogPrompt(searchableMap)
				if catalog != "" {
					rc.SystemPrompt = strings.TrimRight(rc.SystemPrompt, "\n") + "\n" + catalog
				}
			}
			allSpecs = coreSpecs
		}

		rc.ToolExecutor = executor
		rc.FinalSpecs = allSpecs
		rc.ReadCapabilities = ResolveReadCapabilities(
			rc.SelectedRoute,
			rc.FinalSpecs,
			rc.ActiveToolProviderByGroup,
		)
		toolNames := make([]string, 0, len(rc.FinalSpecs))
		for _, spec := range rc.FinalSpecs {
			if name := strings.TrimSpace(spec.Name); name != "" {
				toolNames = append(toolNames, name)
			}
		}
		emitTraceEvent(rc, "tool_build", "tool_build.completed", map[string]any{
			"final_tool_count": len(rc.FinalSpecs),
			"tool_names":       traceToolNames(toolNames),
		})

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
	// load_tools and load_skill are always core when core_tools is configured
	set["load_tools"] = struct{}{}
	set["load_skill"] = struct{}{}
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
// allowing load_tools executor to be created before the DispatchingExecutor exists.
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

func filterAllowlistByRuntime(allowlistSet map[string]struct{}, snapshot *sharedtoolruntime.RuntimeSnapshot, registry *tools.Registry, activeByGroup map[string]string) map[string]struct{} {
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
		group := resolveProviderBindingGroup(registry, name)
		if group != "" {
			if _, ok := activeByGroup[group]; ok {
				continue
			}
		}
		delete(out, name)
	}
	return out
}

func FilterAllowlistByRuntime(allowlistSet map[string]struct{}, snapshot *sharedtoolruntime.RuntimeSnapshot, registry *tools.Registry, activeByGroup map[string]string) map[string]struct{} {
	return filterAllowlistByRuntime(allowlistSet, snapshot, registry, activeByGroup)
}

// identityRequiredTools are tools that need a valid UserID to function.
var identityRequiredTools = map[string]struct{}{
	"memory_search":   {},
	"memory_read":     {},
	"memory_write":    {},
	"memory_edit":     {},
	"memory_forget":   {},
	"notebook_read":   {},
	"notebook_write":  {},
	"notebook_edit":   {},
	"notebook_forget": {},
}

func filterIdentityRequiredTools(allowlistSet map[string]struct{}) map[string]struct{} {
	out := CopyStringSet(allowlistSet)
	for name := range identityRequiredTools {
		delete(out, name)
	}
	return out
}
