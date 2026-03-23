package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/platform"
)

// NewPlatformMiddleware 合并 call_platform 和 platform_manage 两个工具的条件注入。
//
// call_platform: SubAgentControl 可用 + AllowPlatformDelegation + 用户持有 admin 角色时注入。
// platform_manage: platform persona 的子 run 注入。
func NewPlatformMiddleware(platformToolExecutor tools.Executor) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		// call_platform
		if shouldInjectCallPlatform(ctx, rc) {
			cpExec := &platform.CallPlatformExecutor{
				Control: rc.SubAgentControl,
			}
			cpSpec := platform.CallPlatformAgentSpec
			rc.ToolExecutors[cpSpec.Name] = cpExec
			rc.AllowlistSet[cpSpec.Name] = struct{}{}
			rc.ToolSpecs = append(rc.ToolSpecs, platform.CallPlatformLlmSpec)
			rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, []tools.AgentToolSpec{cpSpec})
		}

		// platform_manage
		if shouldInjectPlatformTools(rc, platformToolExecutor) {
			ptSpec := platform.AgentSpec
			rc.ToolExecutors[ptSpec.Name] = platformToolExecutor
			rc.AllowlistSet[ptSpec.Name] = struct{}{}
			rc.ToolSpecs = append(rc.ToolSpecs, platform.LlmSpec)
			rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, []tools.AgentToolSpec{ptSpec})
		}

		return next(ctx, rc)
	}
}

func shouldInjectCallPlatform(ctx context.Context, rc *RunContext) bool {
	if rc.SubAgentControl == nil {
		return false
	}

	if rc.PersonaDefinition == nil || !rc.PersonaDefinition.AllowPlatformDelegation {
		return false
	}

	if rc.Run.CreatedByUserID == nil || rc.Pool == nil {
		return false
	}

	repo := data.AccountMembershipsRepository{}
	membership, err := repo.GetByAccountAndUser(ctx, rc.Pool, rc.Run.AccountID, *rc.Run.CreatedByUserID)
	if err != nil {
		slog.WarnContext(ctx, "call_platform: failed to query membership", "error", err)
		return false
	}
	if membership == nil {
		return false
	}
	return membership.Role == "account_admin" || membership.Role == "platform_admin"
}

func shouldInjectPlatformTools(rc *RunContext, executor tools.Executor) bool {
	if executor == nil || rc.PersonaDefinition == nil {
		return false
	}
	return rc.PersonaDefinition.IsSystem && rc.PersonaDefinition.ID == "platform"
}
