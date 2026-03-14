package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
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

		executor := &spawnagent.ToolExecutor{
			Control:     rc.SubAgentControl,
			PersonaKeys: loadPersonaKeys(ctx, rc),
		}
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

// loadPersonaKeys 从 DB 加载当前 account 可用的 persona ID 列表
func loadPersonaKeys(ctx context.Context, rc *RunContext) []string {
	if rc.Pool == nil {
		return nil
	}
	defs, err := personas.LoadFromDB(ctx, rc.Pool, rc.Run.ProjectID)
	if err != nil {
		slog.WarnContext(ctx, "spawn_agent: failed to load persona keys", "error", err)
		return nil
	}
	keys := make([]string, len(defs))
	for i, d := range defs {
		keys[i] = d.ID
	}
	return keys
}
