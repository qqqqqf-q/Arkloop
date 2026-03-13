package pipeline

import (
	"context"
	"strings"

	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPersonaResolutionMiddleware(
	getBaseRegistry func() *personas.Registry,
	dbPool *pgxpool.Pool,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	releaseSlot func(ctx context.Context, run data.Run),
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		runPersonaRegistry := personas.NewRegistry()
		if dbPool != nil {
			dbDefs, dbErr := personas.LoadFromDB(ctx, dbPool, rc.Run.ProjectID)
			if dbErr != nil {
				payload := map[string]any{
					"error_class": "internal.error",
					"message":     "persona registry load failed",
					"details":     map[string]any{"reason": dbErr.Error()},
				}
				failed := rc.Emitter.Emit("run.failed", payload, nil, StringPtr("internal.error"))
				var releaseFn func()
				if releaseSlot != nil {
					run := rc.Run
					releaseFn = func() { releaseSlot(ctx, run) }
				}
				return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB)
			}
			for _, def := range dbDefs {
				runPersonaRegistry.Set(def)
			}
		} else if getBaseRegistry != nil {
			runPersonaRegistry = getBaseRegistry()
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
		rc.ToolDenylist = nil
		rc.PersonaDefinition = resolution.Definition
		rc.AgentConfig = nil
		rc.AgentConfigID = nil
		rc.AgentConfigName = ""

		normalizedLimits := sharedexec.NormalizePlatformLimits(sharedexec.PlatformLimits{
			AgentReasoningIterations: rc.AgentReasoningIterationsLimit,
			ToolContinuationBudget:   rc.ToolContinuationBudgetLimit,
		})
		rc.AgentReasoningIterationsLimit = normalizedLimits.AgentReasoningIterations
		rc.ToolContinuationBudgetLimit = normalizedLimits.ToolContinuationBudget

		if resolution.Definition != nil {
			def := resolution.Definition
			rc.AgentConfig = &ResolvedAgentConfig{
				Model:              def.Model,
				PromptCacheControl: def.PromptCacheControl,
				ReasoningMode:      def.ReasoningMode,
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
		rc.MaxCostMicros = profile.MaxCostMicros
		rc.MaxTotalOutputTokens = profile.MaxTotalOutputTokens
		rc.PreferredCredentialName = profile.PreferredCredentialName

		if resolution.Definition != nil {
			def := resolution.Definition
			rc.ToolDenylist = append([]string(nil), def.ToolDenylist...)
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
		SoulMD:                  def.SoulMD,
		PromptMD:                joinPromptSegments(def.PromptMD, def.RoleSoulMD, def.RolePromptMD),
		PreferredCredentialName: def.PreferredCredential,
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

func joinPromptSegments(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		segments = append(segments, trimmed)
	}
	return strings.Join(segments, "\n\n")
}
