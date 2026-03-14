package pipeline

import (
	"context"
	"log/slog"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/tools"
)

var runtimeManagedToolNames = map[string]struct{}{
	"browser":             {},
	"conversation_search": {},
	"document_write":      {},
	"exec_command":        {},
	"memory_forget":       {},
	"memory_read":         {},
	"memory_search":       {},
	"memory_write":        {},
	"python_execute":      {},
	"write_stdin":         {},
}

// NewToolBuildMiddleware 根据最终的 allowlist 构建 DispatchingExecutor 和过滤后的 ToolSpecs。
// 当 persona 定义了 tool_allowlist 时，进一步收窄到 persona 允许的工具集。
func NewToolBuildMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		effectiveAllowlist := CopyStringSet(rc.AllowlistSet)

		resolvedAllowlist, err := ResolveProviderAllowlist(effectiveAllowlist, rc.ToolRegistry, rc.ActiveToolProviderByGroup)
		if err != nil {
			return err
		}
		resolvedAllowlist = filterAllowlistByRuntime(resolvedAllowlist, rc.Runtime)

		filteredAllowlist, dropped := FilterAllowlistToBoundExecutors(resolvedAllowlist, rc.ToolExecutors)
		if len(dropped) > 0 {
			slog.WarnContext(ctx, "tool allowlist dropped unbound executors", "run_id", rc.Run.ID, "tools", dropped)
		}

		filteredAllowlist = filterNotConfiguredExecutors(filteredAllowlist, rc.ToolExecutors)

		executor, err := BuildDispatchExecutor(rc.ToolRegistry, rc.ToolExecutors, filteredAllowlist)
		if err != nil {
			return err
		}

		rc.ToolExecutor = executor
		rc.FinalSpecs = FilterToolSpecs(rc.ToolSpecs, filteredAllowlist, rc.ToolRegistry)

		return next(ctx, rc)
	}
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
