package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/skills"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultAgentMaxIterations = 10

// NewSkillResolutionMiddleware 加载 org skills 并解析 skill_id，设置 RunContext 的 skill 相关字段。
// skill 解析失败时写入 run.failed 并短路。
func NewSkillResolutionMiddleware(
	baseSkillRegistry *skills.Registry,
	dbPool *pgxpool.Pool,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	releaseSlot func(ctx context.Context, run data.Run),
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		// per-run 动态加载 org skill
		runSkillRegistry := baseSkillRegistry
		if dbPool != nil {
			dbDefs, dbErr := skills.LoadFromDB(ctx, dbPool, rc.Run.OrgID)
			if dbErr != nil {
				slog.WarnContext(ctx, "skills: db load failed, using static registry", "err", dbErr.Error())
			} else if len(dbDefs) > 0 {
				runSkillRegistry = skills.MergeRegistry(baseSkillRegistry, dbDefs)
			}
		}

		resolution := skills.ResolveSkill(rc.InputJSON, runSkillRegistry)
		if resolution.Error != nil {
			payload := map[string]any{
				"error_class": resolution.Error.ErrorClass,
				"message":     resolution.Error.Message,
			}
			if len(resolution.Error.Details) > 0 {
				payload["details"] = resolution.Error.Details
			}
			failed := rc.Emitter.Emit(
				"run.failed",
				payload,
				nil,
				StringPtr(resolution.Error.ErrorClass),
			)
			var releaseFn func()
			if releaseSlot != nil {
				run := rc.Run
				releaseFn = func() { releaseSlot(ctx, run) }
			}
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB)
		}

		rc.MaxIterations = defaultAgentMaxIterations
		rc.ToolBudget = map[string]any{}
		rc.SkillDefinition = resolution.Definition

		// AgentConfigMiddleware 已解析的配置作为基础 fallback
		if rc.AgentConfig != nil {
			if rc.AgentConfig.SystemPrompt != nil {
				rc.SystemPrompt = *rc.AgentConfig.SystemPrompt
			}
			if rc.AgentConfig.MaxOutputTokens != nil {
				rc.MaxOutputTokens = rc.AgentConfig.MaxOutputTokens
			}
			// 将 agent_config 的工具策略应用到 allowlist
			switch rc.AgentConfig.ToolPolicy {
			case "allowlist":
				if len(rc.AgentConfig.ToolAllowlist) > 0 {
					narrowed := make(map[string]struct{}, len(rc.AgentConfig.ToolAllowlist))
					for _, name := range rc.AgentConfig.ToolAllowlist {
						if _, ok := rc.AllowlistSet[name]; ok {
							narrowed[name] = struct{}{}
						}
					}
					rc.AllowlistSet = narrowed
				}
			case "denylist":
				for _, name := range rc.AgentConfig.ToolDenylist {
					delete(rc.AllowlistSet, name)
				}
			}
			// "none" 不限制，rc.AllowlistSet 保持不变
		}

		// skill 定义覆盖 agent_config 的对应字段
		if resolution.Definition != nil {
			def := resolution.Definition
			rc.SystemPrompt = def.PromptMD
			if def.Budgets.MaxIterations != nil {
				rc.MaxIterations = *def.Budgets.MaxIterations
			}
			rc.MaxOutputTokens = def.Budgets.MaxOutputTokens
			rc.ToolTimeoutMs = def.Budgets.ToolTimeoutMs
			for key, value := range def.Budgets.ToolBudget {
				rc.ToolBudget[key] = value
			}
			// 用户未显式指定 route_id 时，Skill 声明的偏好路由作为第二优先级
			if def.PreferredRouteID != nil {
				if _, hasRouteID := rc.InputJSON["route_id"]; !hasRouteID {
					rc.InputJSON["route_id"] = *def.PreferredRouteID
				}
			}
		}

		return next(ctx, rc)
	}
}
