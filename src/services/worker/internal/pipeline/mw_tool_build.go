package pipeline

import "context"

// NewToolBuildMiddleware 根据最终的 allowlist 构建 DispatchingExecutor 和过滤后的 ToolSpecs。
// 当 persona 定义了 tool_allowlist 时，进一步收窄到 persona 允许的工具集。
func NewToolBuildMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		effectiveAllowlist := CopyStringSet(rc.AllowlistSet)

		if rc.PersonaDefinition != nil && len(rc.PersonaDefinition.ToolAllowlist) > 0 {
			effectiveAllowlist = make(map[string]struct{})
			for _, name := range rc.PersonaDefinition.ToolAllowlist {
				if _, ok := rc.AllowlistSet[name]; !ok {
					continue
				}
				effectiveAllowlist[name] = struct{}{}
			}
		}

		executor, err := BuildDispatchExecutor(rc.ToolRegistry, rc.ToolExecutors, effectiveAllowlist)
		if err != nil {
			return err
		}

		rc.ToolExecutor = executor
		rc.FinalSpecs = FilterToolSpecs(rc.ToolSpecs, effectiveAllowlist)

		return next(ctx, rc)
	}
}
