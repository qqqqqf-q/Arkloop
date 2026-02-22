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
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn)
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
		}

		return next(ctx, rc)
	}
}
