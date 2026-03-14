package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
)

// NewMCPDiscoveryMiddleware 按 account 从 DB 加载 MCP 工具（带缓存），合并到 RunContext 的工具集。
func NewMCPDiscoveryMiddleware(
	discoveryCache *mcp.DiscoveryCache,
	baseToolExecutors map[string]tools.Executor,
	baseAllLlmSpecs []llm.ToolSpec,
	baseAllowlistSet map[string]struct{},
	baseRegistry *tools.Registry,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		runToolExecutors := CopyToolExecutors(baseToolExecutors)
		runAllLlmSpecs := append([]llm.ToolSpec{}, baseAllLlmSpecs...)
		runAllowlistSet := CopyStringSet(baseAllowlistSet)
		runRegistry := baseRegistry

		if discoveryCache != nil {
			accountReg, accountErr := discoveryCache.Get(ctx, rc.Pool, rc.Run.AccountID)
			if accountErr != nil {
				slog.WarnContext(ctx, "mcp discovery failed, falling back to base tools", "account_id", rc.Run.AccountID, "err", accountErr)
			}
			if accountErr == nil && len(accountReg.Executors) > 0 {
				// 过滤与内置 spawn_agent 系列同名的 MCP 工具，避免后续注册冲突
				filteredSpecs := filterBuiltinConflicts(accountReg.AgentSpecs)
				runRegistry = ForkRegistry(baseRegistry, filteredSpecs)
				for name, exec := range accountReg.Executors {
					if _, builtin := spawnagent.BuiltinNames[name]; builtin {
						continue
					}
					runToolExecutors[name] = exec
				}
				for _, spec := range accountReg.LlmSpecs {
					if _, builtin := spawnagent.BuiltinNames[spec.Name]; builtin {
						continue
					}
					runAllLlmSpecs = append(runAllLlmSpecs, spec)
				}
				for _, spec := range filteredSpecs {
					runAllowlistSet[spec.Name] = struct{}{}
				}
			}
		}

		rc.ToolExecutors = runToolExecutors
		rc.ToolSpecs = runAllLlmSpecs
		rc.AllowlistSet = runAllowlistSet
		rc.ToolRegistry = runRegistry

		return next(ctx, rc)
	}
}

// filterBuiltinConflicts 移除与内置 spawn_agent 工具同名的 MCP spec
func filterBuiltinConflicts(specs []tools.AgentToolSpec) []tools.AgentToolSpec {
	out := make([]tools.AgentToolSpec, 0, len(specs))
	for _, spec := range specs {
		if _, builtin := spawnagent.BuiltinNames[spec.Name]; builtin {
			slog.Debug("mcp tool shadowed by builtin, skipped", "name", spec.Name)
			continue
		}
		out = append(out, spec)
	}
	return out
}
