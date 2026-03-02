package pipeline

import (
	"context"
	"log/slog"
)

// NewToolBuildMiddleware 根据最终的 allowlist 构建 DispatchingExecutor 和过滤后的 ToolSpecs。
// 当 persona 定义了 tool_allowlist 时，进一步收窄到 persona 允许的工具集。
func NewToolBuildMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		effectiveAllowlist := CopyStringSet(rc.AllowlistSet)

		resolvedAllowlist, err := ResolveProviderAllowlist(effectiveAllowlist, rc.ToolRegistry, rc.ActiveToolProviderByGroup)
		if err != nil {
			return err
		}

		filteredAllowlist, dropped := FilterAllowlistToBoundExecutors(resolvedAllowlist, rc.ToolExecutors)
		if len(dropped) > 0 {
			slog.WarnContext(ctx, "tool allowlist dropped unbound executors", "run_id", rc.Run.ID, "tools", dropped)
		}

		executor, err := BuildDispatchExecutor(rc.ToolRegistry, rc.ToolExecutors, filteredAllowlist)
		if err != nil {
			return err
		}

		rc.ToolExecutor = executor
		rc.FinalSpecs = FilterToolSpecs(rc.ToolSpecs, filteredAllowlist, rc.ToolRegistry)

		return next(ctx, rc)
	}
}
