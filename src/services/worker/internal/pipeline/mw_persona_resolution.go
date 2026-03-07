package pipeline

import (
	"context"
	"log/slog"

	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/tools"

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
		rc.PerToolSoftLimits = tools.DefaultPerToolSoftLimits()
		rc.PersonaDefinition = resolution.Definition

		normalizedLimits := sharedexec.NormalizePlatformLimits(sharedexec.PlatformLimits{
			AgentReasoningIterations: rc.AgentReasoningIterationsLimit,
			ToolContinuationBudget:   rc.ToolContinuationBudgetLimit,
		})
		rc.AgentReasoningIterationsLimit = normalizedLimits.AgentReasoningIterations
		rc.ToolContinuationBudgetLimit = normalizedLimits.ToolContinuationBudget

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

		profile := sharedexec.ResolveEffectiveProfile(
			normalizedLimits,
			toExecutionAgentConfigProfile(rc.AgentConfig, rc.AgentConfigName),
			toExecutionPersonaProfile(resolution.Definition),
		)

		rc.SystemPrompt = profile.SystemPrompt
		rc.ReasoningIterations = profile.ReasoningIterations
		rc.ToolContinuationBudget = profile.ToolContinuationBudget
		rc.MaxOutputTokens = profile.MaxOutputTokens
		rc.Temperature = profile.Temperature
		rc.TopP = profile.TopP
		rc.ReasoningMode = profile.ReasoningMode
		rc.ToolTimeoutMs = profile.ToolTimeoutMs
		rc.ToolBudget = profile.ToolBudget
		rc.PerToolSoftLimits = tools.CopyPerToolSoftLimits(profile.PerToolSoftLimits)
		rc.PreferredCredentialName = profile.PreferredCredentialName

		if rc.AgentConfig != nil {
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

		if resolution.Definition != nil {
			def := resolution.Definition
			if len(def.ToolAllowlist) > 0 {
				narrowed := make(map[string]struct{}, len(def.ToolAllowlist))
				for _, name := range def.ToolAllowlist {
					if ToolAllowed(rc.AllowlistSet, rc.ToolRegistry, name) {
						narrowed[name] = struct{}{}
					}
				}
				rc.AllowlistSet = narrowed
			}
			for _, name := range def.ToolDenylist {
				RemoveToolOrGroup(rc.AllowlistSet, rc.ToolRegistry, name)
			}
			rc.TitleSummarizer = def.TitleSummarizer
		}

		return next(ctx, rc)
	}
}

func toExecutionAgentConfigProfile(ac *ResolvedAgentConfig, name string) *sharedexec.AgentConfigProfile {
	if ac == nil {
		return nil
	}
	return &sharedexec.AgentConfigProfile{
		Name:            name,
		SystemPrompt:    ac.SystemPrompt,
		Temperature:     ac.Temperature,
		MaxOutputTokens: ac.MaxOutputTokens,
		TopP:            ac.TopP,
		ReasoningMode:   ac.ReasoningMode,
	}
}

func toExecutionPersonaProfile(def *personas.Definition) *sharedexec.PersonaProfile {
	if def == nil {
		return nil
	}
	return &sharedexec.PersonaProfile{
		PromptMD:                def.PromptMD,
		PreferredCredentialName: def.PreferredCredential,
		ResolvedAgentConfigName: def.AgentConfigName,
		Budgets: sharedexec.RequestedBudgets{
			ReasoningIterations:    def.Budgets.ReasoningIterations,
			ToolContinuationBudget: def.Budgets.ToolContinuationBudget,
			MaxOutputTokens:        def.Budgets.MaxOutputTokens,
			ToolTimeoutMs:          def.Budgets.ToolTimeoutMs,
			ToolBudget:             def.Budgets.ToolBudget,
			PerToolSoftLimits:      def.Budgets.PerToolSoftLimits,
			Temperature:            def.Budgets.Temperature,
			TopP:                   def.Budgets.TopP,
		},
	}
}
