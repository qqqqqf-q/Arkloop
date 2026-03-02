package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPersonaResolutionMiddleware 加载 org personas 并解析 persona_id，设置 RunContext 的 persona 相关字段。
// persona 解析失败时写入 run.failed 并短路。
// getBaseRegistry 每次 run 调用时获取最新 registry，支持热重载。
func NewPersonaResolutionMiddleware(
	getBaseRegistry func() *personas.Registry,
	dbPool *pgxpool.Pool,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	releaseSlot func(ctx context.Context, run data.Run),
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		basePersonaRegistry := getBaseRegistry()
		// per-run 动态加载 org persona
		runPersonaRegistry := basePersonaRegistry
		if dbPool != nil {
			dbDefs, dbErr := personas.LoadFromDB(ctx, dbPool, rc.Run.OrgID)
			if dbErr != nil {
				slog.WarnContext(ctx, "personas: db load failed, using static registry", "err", dbErr.Error())
			} else if len(dbDefs) > 0 {
				runPersonaRegistry = personas.MergeRegistry(basePersonaRegistry, dbDefs)
			}
		}

		resolution := personas.ResolvePersona(rc.InputJSON, runPersonaRegistry)
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

		rc.ToolBudget = map[string]any{}
		rc.PersonaDefinition = resolution.Definition

		maxIterLimit := rc.AgentMaxIterationsLimit
		if maxIterLimit <= 0 {
			maxIterLimit = 10
		}
		rc.AgentMaxIterationsLimit = maxIterLimit
		rc.MaxIterations = maxIterLimit

		// 若 persona 显式绑定了 AgentConfig，按名称覆盖继承链解析结果
		if resolution.Definition != nil && resolution.Definition.AgentConfigName != nil && dbPool != nil {
			ac, acName, err := loadAgentConfigByName(ctx, dbPool, *resolution.Definition.AgentConfigName, rc.Run.OrgID)
			if err != nil {
				slog.WarnContext(ctx, "persona: agent_config_name lookup failed",
					"persona_id", resolution.Definition.ID,
					"agent_config_name", *resolution.Definition.AgentConfigName,
					"err", err.Error(),
				)
			} else if ac != nil {
				rc.AgentConfig = ac
				rc.AgentConfigName = acName
				rc.AgentConfigID = nil
			}
		}

		// -- 分层遮罩逻辑 --
		// 1. AgentConfig 提供基线配置（模型、凭证、安全约束、SystemPrompt 前缀）
		// 2. Persona 在 AgentConfig 约束内设置执行参数（prompt 追加、预算、温度）
		// 3. 工具策略：AgentConfig 定义可用池，Persona 从池中选取（交集）
		// 4. MaxOutputTokens：AgentConfig 设上界，Persona 可以设更小值但不能超过

		var agentConfigPromptPrefix string
		var agentConfigMaxOutputTokens *int

		if rc.AgentConfig != nil {
			// AgentConfig 的 SystemPrompt 作为前缀基础
			if rc.AgentConfig.SystemPrompt != nil {
				agentConfigPromptPrefix = *rc.AgentConfig.SystemPrompt
			}
			// AgentConfig 的 MaxOutputTokens 作为上界
			agentConfigMaxOutputTokens = rc.AgentConfig.MaxOutputTokens
			// AgentConfig 的 Temperature/TopP 作为 fallback
			rc.Temperature = rc.AgentConfig.Temperature
			rc.TopP = rc.AgentConfig.TopP
			rc.ReasoningMode = rc.AgentConfig.ReasoningMode

			// AgentConfig 的工具策略始终生效，定义可用工具池
			switch rc.AgentConfig.ToolPolicy {
			case "allowlist":
				if len(rc.AgentConfig.ToolAllowlist) > 0 {
					narrowed := make(map[string]struct{}, len(rc.AgentConfig.ToolAllowlist))
					for _, name := range rc.AgentConfig.ToolAllowlist {
						if ToolAllowed(rc.AllowlistSet, rc.ToolRegistry, name) {
							narrowed[name] = struct{}{}
						}
					}
					rc.AllowlistSet = narrowed
				}
			case "denylist":
				for _, name := range rc.AgentConfig.ToolDenylist {
					RemoveToolOrGroup(rc.AllowlistSet, rc.ToolRegistry, name)
				}
			}
		}

		// Persona 在 AgentConfig 约束内设置执行参数
		if resolution.Definition != nil {
			def := resolution.Definition

			// SystemPrompt：AgentConfig 前缀 + Persona prompt 追加
			if agentConfigPromptPrefix != "" && def.PromptMD != "" {
				rc.SystemPrompt = agentConfigPromptPrefix + "\n\n" + def.PromptMD
			} else if def.PromptMD != "" {
				rc.SystemPrompt = def.PromptMD
			} else {
				rc.SystemPrompt = agentConfigPromptPrefix
			}

			if def.Budgets.MaxIterations != nil {
				if v := *def.Budgets.MaxIterations; v > 0 && v < maxIterLimit {
					rc.MaxIterations = v
				}
			}

			// MaxOutputTokens：取 Persona 值，但不超过 AgentConfig 上界
			if def.Budgets.MaxOutputTokens != nil {
				if agentConfigMaxOutputTokens != nil && *def.Budgets.MaxOutputTokens > *agentConfigMaxOutputTokens {
					rc.MaxOutputTokens = agentConfigMaxOutputTokens
				} else {
					rc.MaxOutputTokens = def.Budgets.MaxOutputTokens
				}
			} else {
				rc.MaxOutputTokens = agentConfigMaxOutputTokens
			}

			// Temperature/TopP：Persona 设置优先（在合理范围内）
			if def.Budgets.Temperature != nil {
				rc.Temperature = def.Budgets.Temperature
			}
			if def.Budgets.TopP != nil {
				rc.TopP = def.Budgets.TopP
			}

			rc.ToolTimeoutMs = def.Budgets.ToolTimeoutMs
			for key, value := range def.Budgets.ToolBudget {
				rc.ToolBudget[key] = value
			}

			// Persona 的 tool_allowlist 从 AgentConfig 已缩窄的池中取交集
			if len(def.ToolAllowlist) > 0 {
				narrowed := make(map[string]struct{}, len(def.ToolAllowlist))
				for _, name := range def.ToolAllowlist {
					if ToolAllowed(rc.AllowlistSet, rc.ToolRegistry, name) {
						narrowed[name] = struct{}{}
					}
				}
				rc.AllowlistSet = narrowed
			}

			// Persona 的 tool_denylist 从当前池中排除
			for _, name := range def.ToolDenylist {
				RemoveToolOrGroup(rc.AllowlistSet, rc.ToolRegistry, name)
			}

			if def.PreferredCredential != nil {
				rc.PreferredCredentialName = *def.PreferredCredential
			}

			rc.TitleSummarizer = def.TitleSummarizer
		} else {
			// 无 persona 定义时，使用 AgentConfig 的值
			rc.SystemPrompt = agentConfigPromptPrefix
			rc.MaxOutputTokens = agentConfigMaxOutputTokens
		}

		return next(ctx, rc)
	}
}
