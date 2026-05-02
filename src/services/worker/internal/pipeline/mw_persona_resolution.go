package pipeline

import (
	"context"
	"log/slog"
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
				if rc.ReleaseSlot != nil {
					releaseFn = rc.ReleaseSlot
				} else if releaseSlot != nil {
					run := rc.Run
					releaseFn = func() { releaseSlot(ctx, run) }
				}
				return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus)
			}
			if getBaseRegistry != nil {
				runPersonaRegistry = personas.MergeRegistry(getBaseRegistry(), dbDefs)
			} else {
				for _, def := range dbDefs {
					runPersonaRegistry.Set(def)
				}
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
			if rc.ReleaseSlot != nil {
				releaseFn = rc.ReleaseSlot
			} else if releaseSlot != nil {
				run := rc.Run
				releaseFn = func() { releaseSlot(ctx, run) }
			}
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus)
		}
		rc.ToolBudget = map[string]any{}
		rc.PerToolSoftLimits = tools.DefaultPerToolSoftLimits()
		rc.ToolDenylist = nil
		rc.StreamThinking = true
		rc.SummarizerDefinition = nil
		rc.TitleSummarizer = nil
		rc.ResultSummarizer = nil
		rc.PersonaDefinition = resolution.Definition
		rc.AgentConfig = nil
		rc.AgentConfigID = nil
		rc.AgentConfigName = ""

		// 版本漂移检测：persona 在 run 创建后被修改
		if resolution.Definition != nil &&
			!resolution.Definition.UpdatedAt.IsZero() &&
			!rc.Run.CreatedAt.IsZero() &&
			resolution.Definition.UpdatedAt.After(rc.Run.CreatedAt) {
			slog.WarnContext(ctx, "persona version drift detected: persona modified after run creation",
				"run_id", rc.Run.ID,
				"persona_id", resolution.Definition.ID,
				"persona_version", resolution.Definition.Version,
				"run_created_at", rc.Run.CreatedAt,
				"persona_updated_at", resolution.Definition.UpdatedAt,
			)
		}

		normalizedLimits := sharedexec.NormalizePlatformLimits(sharedexec.PlatformLimits{
			AgentReasoningIterations: rc.AgentReasoningIterationsLimit,
			ToolContinuationBudget:   rc.ToolContinuationBudgetLimit,
		})
		rc.AgentReasoningIterationsLimit = normalizedLimits.AgentReasoningIterations
		rc.ToolContinuationBudgetLimit = normalizedLimits.ToolContinuationBudget

		if resolution.Definition != nil {
			def := resolution.Definition
			rc.StreamThinking = def.StreamThinking
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

		if rc.ResumePromptSnapshot != nil {
			rc.ApplyResumePromptSnapshot()
		} else {
			rc.ResetPromptAssembly()
			cacheSystemPrompt := rc.AgentConfig != nil && rc.AgentConfig.PromptCacheControl == "system_prompt"
			rc.UpsertPromptSegment(PromptSegment{
				Name:          "persona.system_prompt",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          profile.SystemPrompt,
				Stability:     PromptStabilityStablePrefix,
				CacheEligible: cacheSystemPrompt,
			})
			if len(rc.PendingSubAgentCallbacks) > 0 {
				rc.UpsertPromptSegment(PromptSegment{
					Name:          "runtime.pending_subagent_callbacks",
					Target:        PromptTargetRuntimeTail,
					Role:          "user",
					Text:          buildPendingSubAgentCallbacksBlock(rc.PendingSubAgentCallbacks),
					Stability:     PromptStabilityVolatileTail,
					CacheEligible: false,
				})
			}
		}
		rc.ReasoningIterations = profile.ReasoningIterations
		rc.ToolContinuationBudget = profile.ToolContinuationBudget
		rc.MaxOutputTokens = profile.MaxOutputTokens
		rc.Temperature = profile.Temperature
		rc.TopP = profile.TopP
		rc.ReasoningMode = profile.ReasoningMode
		if override := normalizeRunReasoningModeOverride(rc.InputJSON["reasoning_mode"]); override != "" {
			rc.ReasoningMode = override
			if rc.AgentConfig != nil {
				rc.AgentConfig.ReasoningMode = override
			}
		}
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
		}

		if runPersonaRegistry != nil {
			if summarizerDef, ok := runPersonaRegistry.Get(personas.SystemSummarizerPersonaID); ok {
				summaryClone := summarizerDef
				rc.SummarizerDefinition = &summaryClone
				rc.TitleSummarizer = summarizerDef.TitleSummarizer
				rc.ResultSummarizer = summarizerDef.ResultSummarizer
			}
		}
		SyncPlanModePrompt(rc)
		SyncLearningModePrompt(rc)

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

func normalizeRunReasoningModeOverride(raw any) string {
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto":
		return "auto"
	case "enabled":
		return "enabled"
	case "disabled":
		return "disabled"
	case "none", "off":
		return "none"
	case "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "max", "xhigh", "extra_high", "extra-high", "extra high":
		return "xhigh"
	default:
		return ""
	}
}
