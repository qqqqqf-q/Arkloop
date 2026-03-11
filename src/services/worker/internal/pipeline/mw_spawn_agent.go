package pipeline

import (
	"context"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
)

// NewSpawnAgentMiddleware 在 SubAgentControl 可用时，将 sub-agent 控制工具动态注入到 per-run 工具集。
// 位于 MCPDiscovery 之后、AgentConfig 之前，使后续 PersonaResolution 的 denylist 能正常排除。
func NewSpawnAgentMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc.SubAgentControl == nil {
			return next(ctx, rc)
		}

		executor := &spawnagent.ToolExecutor{Control: rc.SubAgentControl}
		specs := []tools.AgentToolSpec{
			spawnagent.AgentSpec,
			spawnagent.SendInputSpec,
			spawnagent.WaitAgentSpec,
			spawnagent.ResumeAgentSpec,
			spawnagent.CloseAgentSpec,
			spawnagent.InterruptAgentSpec,
		}
		llmSpecs := []llm.ToolSpec{
			spawnagent.LlmSpec,
			spawnagent.SendInputLlmSpec,
			spawnagent.WaitAgentLlmSpec,
			spawnagent.ResumeAgentLlmSpec,
			spawnagent.CloseAgentLlmSpec,
			spawnagent.InterruptAgentLlmSpec,
		}
		for _, spec := range specs {
			rc.ToolExecutors[spec.Name] = executor
			rc.AllowlistSet[spec.Name] = struct{}{}
		}
		rc.ToolSpecs = append(rc.ToolSpecs, llmSpecs...)
		rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, specs)
		return next(ctx, rc)
	}
}
