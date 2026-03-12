package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/tools"
)

// NewMCPDiscoveryMiddleware 按 org 从 DB 加载 MCP 工具（带缓存），合并到 RunContext 的工具集。
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
			orgReg, orgErr := discoveryCache.Get(ctx, rc.Pool, rc.Run.AccountID)
			if orgErr != nil {
				slog.WarnContext(ctx, "mcp discovery failed, falling back to base tools", "account_id", rc.Run.AccountID, "err", orgErr)
			}
			if orgErr == nil && len(orgReg.Executors) > 0 {
				runRegistry = ForkRegistry(baseRegistry, orgReg.AgentSpecs)
				for name, exec := range orgReg.Executors {
					runToolExecutors[name] = exec
				}
				runAllLlmSpecs = append(runAllLlmSpecs, orgReg.LlmSpecs...)
				for _, spec := range orgReg.AgentSpecs {
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
