package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/platform"
)

// NewCallPlatformMiddleware 条件注入 call_platform 工具。
// 同时满足以下条件时注入：
//  1. SubAgentControl 可用
//  2. 当前 Persona 的 AllowPlatformDelegation 为 true
//  3. 当前用户持有 account_admin 或 platform_admin 角色
func NewCallPlatformMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if !shouldInjectCallPlatform(ctx, rc) {
			return next(ctx, rc)
		}

		executor := &platform.CallPlatformExecutor{
			Control: rc.SubAgentControl,
		}
		spec := platform.CallPlatformAgentSpec
		rc.ToolExecutors[spec.Name] = executor
		rc.AllowlistSet[spec.Name] = struct{}{}
		rc.ToolSpecs = append(rc.ToolSpecs, platform.CallPlatformLlmSpec)
		rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, []tools.AgentToolSpec{spec})

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
