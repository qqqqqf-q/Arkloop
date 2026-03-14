package pipeline

import (
	"context"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/platform"
)

// NewPlatformToolsMiddleware 为 platform persona 的子 run 注入 platform_manage 工具。
// executor 在 composition 阶段创建，通过闭包传入。
func NewPlatformToolsMiddleware(executor tools.Executor) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if executor == nil || rc.PersonaDefinition == nil {
			return next(ctx, rc)
		}
		if !rc.PersonaDefinition.IsSystem || rc.PersonaDefinition.ID != "platform" {
			return next(ctx, rc)
		}

		spec := platform.AgentSpec
		rc.ToolExecutors[spec.Name] = executor
		rc.AllowlistSet[spec.Name] = struct{}{}
		rc.ToolSpecs = append(rc.ToolSpecs, platform.LlmSpec)
		rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, []tools.AgentToolSpec{spec})

		return next(ctx, rc)
	}
}
