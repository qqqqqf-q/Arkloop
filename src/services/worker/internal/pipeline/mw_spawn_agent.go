package pipeline

import (
	"context"

	"arkloop/services/worker/internal/tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
)

// NewSpawnAgentMiddleware 在 SpawnChildRun 可用时，将 spawn_agent 工具动态注入到 per-run 工具集。
// 位于 MCPDiscovery 之后、AgentConfig 之前，使后续 SkillResolution 的 denylist 能正常排除。
func NewSpawnAgentMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc.SpawnChildRun == nil {
			return next(ctx, rc)
		}

		executor := &spawnagent.ToolExecutor{SpawnFn: rc.SpawnChildRun}

		rc.ToolExecutors[spawnagent.AgentSpec.Name] = executor
		rc.ToolSpecs = append(rc.ToolSpecs, spawnagent.LlmSpec)
		rc.AllowlistSet[spawnagent.AgentSpec.Name] = struct{}{}
		rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, []tools.AgentToolSpec{spawnagent.AgentSpec})

		return next(ctx, rc)
	}
}
