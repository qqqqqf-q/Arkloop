package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"strconv"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"
	sharedconfig "arkloop/services/shared/config"
	sharedexec "arkloop/services/shared/executionconfig"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var executionGovernanceKeys = []string{
	"limit.agent_reasoning_iterations",
	"limit.tool_continuation_budget",
	"limit.thread_message_history",
	"limit.max_input_content_bytes",
	"limit.max_parallel_tasks",
	"llm.max_response_bytes",
}

type executionGovernanceResponse struct {
	Limits               []sharedconfig.SettingInspection `json:"limits"`
	TitleSummarizerModel *string                          `json:"title_summarizer_model,omitempty"`
	Personas             []executionGovernancePersona     `json:"personas"`
}

type executionGovernancePersona struct {
	ID                  string                              `json:"id"`
	Source              string                              `json:"source"`
	PersonaKey          string                              `json:"persona_key"`
	Version             string                              `json:"version"`
	DisplayName         string                              `json:"display_name"`
	PreferredCredential *string                             `json:"preferred_credential,omitempty"`
	Model               *string                             `json:"model,omitempty"`
	ReasoningMode       string                              `json:"reasoning_mode,omitempty"`
	PromptCacheControl  string                              `json:"prompt_cache_control,omitempty"`
	Requested           sharedexec.RequestedBudgets         `json:"requested"`
	Effective           executionGovernancePersonaEffective `json:"effective"`
}

type executionGovernancePersonaEffective struct {
	SystemPrompt           string                       `json:"system_prompt,omitempty"`
	ReasoningIterations    int                          `json:"reasoning_iterations"`
	ToolContinuationBudget int                          `json:"tool_continuation_budget"`
	MaxOutputTokens        *int                         `json:"max_output_tokens,omitempty"`
	Temperature            *float64                     `json:"temperature,omitempty"`
	TopP                   *float64                     `json:"top_p,omitempty"`
	ReasoningMode          string                       `json:"reasoning_mode,omitempty"`
	PerToolSoftLimits      sharedexec.PerToolSoftLimits `json:"per_tool_soft_limits,omitempty"`
}

func adminExecutionGovernance(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	registry *sharedconfig.Registry,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
		if registry == nil {
			registry = sharedconfig.DefaultRegistry()
		}

		projectID, ok := parseExecutionGovernanceProjectID(w, r, traceID)
		if !ok {
			return
		}

		var store sharedconfig.Store
		if pool != nil {
			store = sharedconfig.NewPGXStoreQuerier(pool)
		}
		scope := sharedconfig.Scope{ProjectID: projectID}
		resp := executionGovernanceResponse{
			Limits:   make([]sharedconfig.SettingInspection, 0, len(executionGovernanceKeys)),
			Personas: []executionGovernancePersona{},
		}

		inspections := make(map[string]sharedconfig.SettingInspection, len(executionGovernanceKeys))
		for _, key := range executionGovernanceKeys {
			inspection, err := sharedconfig.Inspect(r.Context(), registry, store, key, scope)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			resp.Limits = append(resp.Limits, inspection)
			inspections[key] = inspection
		}
		if pool != nil {
			if model, err := loadTitleSummarizerModel(r.Context(), pool); err == nil {
				resp.TitleSummarizerModel = model
			}
		}

		if projectID == nil {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
			return
		}
		if personasRepo == nil {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
			return
		}

		platformLimits := executionGovernancePlatformLimits(inspections)
		customDefs, err := personasRepo.ListByProject(r.Context(), *projectID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		items, err := buildExecutionGovernancePersonas(customDefs, repoPersonas, platformLimits)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		resp.Personas = items
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func parseExecutionGovernanceProjectID(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if raw == "" {
		return nil, true
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid project_id", traceID, nil)
		return nil, false
	}
	return &parsed, true
}

func executionGovernancePlatformLimits(inspections map[string]sharedconfig.SettingInspection) sharedexec.PlatformLimits {
	return sharedexec.PlatformLimits{
		AgentReasoningIterations: inspectionEffectiveInt(inspections["limit.agent_reasoning_iterations"]),
		ToolContinuationBudget:   inspectionEffectiveInt(inspections["limit.tool_continuation_budget"]),
	}
}

func inspectionEffectiveInt(inspection sharedconfig.SettingInspection) int {
	value, err := strconv.Atoi(strings.TrimSpace(inspection.Effective.Value))
	if err != nil {
		return 0
	}
	return value
}

func loadTitleSummarizerModel(ctx context.Context, pool data.DB) (*string, error) {
	if pool == nil {
		return nil, nil
	}
	var value string
	if err := pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`,
		"title_summarizer.model",
	).Scan(&value); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return executionGovernanceOptionalTrimmedString(value), nil
}

func buildExecutionGovernancePersonas(
	customDefs []data.Persona,
	repoDefs []repopersonas.RepoPersona,
	platformLimits sharedexec.PlatformLimits,
) ([]executionGovernancePersona, error) {
	items := make([]executionGovernancePersona, 0, len(customDefs)+len(repoDefs))
	shadowed := make(map[string]struct{}, len(customDefs))
	for _, persona := range customDefs {
		item, err := buildCustomExecutionGovernancePersona(persona, platformLimits)
		if err != nil {
			return nil, err
		}
		shadowed[persona.PersonaKey] = struct{}{}
		items = append(items, item)
	}
	for _, persona := range repoDefs {
		if _, exists := shadowed[persona.ID]; exists {
			continue
		}
		item, err := buildBuiltinExecutionGovernancePersona(persona, platformLimits)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func buildCustomExecutionGovernancePersona(
	persona data.Persona,
	platformLimits sharedexec.PlatformLimits,
) (executionGovernancePersona, error) {
	requested, err := sharedexec.ParseRequestedBudgetsJSON(persona.BudgetsJSON)
	if err != nil {
		return executionGovernancePersona{}, err
	}
	profile := sharedexec.ResolveEffectiveProfile(
		platformLimits,
		governanceAgentProfile(persona.ReasoningMode),
		&sharedexec.PersonaProfile{
			PreferredCredentialName: persona.PreferredCredential,
			PromptMD:                persona.PromptMD,
			Budgets:                 requested,
		},
	)
	return executionGovernancePersona{
		ID:                  persona.ID.String(),
		Source:              "custom",
		PersonaKey:          persona.PersonaKey,
		Version:             persona.Version,
		DisplayName:         persona.DisplayName,
		PreferredCredential: executionGovernanceOptionalTrimmedStringPtr(persona.PreferredCredential),
		Model:               executionGovernanceOptionalTrimmedStringPtr(persona.Model),
		ReasoningMode:       strings.TrimSpace(persona.ReasoningMode),
		PromptCacheControl:  strings.TrimSpace(persona.PromptCacheControl),
		Requested:           requested,
		Effective:           toExecutionGovernancePersonaEffective(profile),
	}, nil
}

func buildBuiltinExecutionGovernancePersona(
	persona repopersonas.RepoPersona,
	platformLimits sharedexec.PlatformLimits,
) (executionGovernancePersona, error) {
	requested, err := sharedexec.ParseRequestedBudgetsMap(persona.Budgets)
	if err != nil {
		return executionGovernancePersona{}, err
	}
	profile := sharedexec.ResolveEffectiveProfile(
		platformLimits,
		governanceAgentProfile(persona.ReasoningMode),
		&sharedexec.PersonaProfile{
			SoulMD:                  persona.SoulMD,
			PreferredCredentialName: executionGovernanceOptionalTrimmedString(persona.PreferredCredential),
			PromptMD:                persona.PromptMD,
			Budgets:                 requested,
		},
	)
	return executionGovernancePersona{
		ID:                  "builtin:" + persona.ID + ":" + persona.Version,
		Source:              "builtin",
		PersonaKey:          persona.ID,
		Version:             persona.Version,
		DisplayName:         persona.Title,
		PreferredCredential: executionGovernanceOptionalTrimmedString(persona.PreferredCredential),
		Model:               executionGovernanceOptionalTrimmedString(persona.Model),
		ReasoningMode:       strings.TrimSpace(persona.ReasoningMode),
		PromptCacheControl:  strings.TrimSpace(persona.PromptCacheControl),
		Requested:           requested,
		Effective:           toExecutionGovernancePersonaEffective(profile),
	}, nil
}

func governanceAgentProfile(reasoningMode string) *sharedexec.AgentConfigProfile {
	cleaned := strings.TrimSpace(reasoningMode)
	if cleaned == "" {
		cleaned = "auto"
	}
	return &sharedexec.AgentConfigProfile{ReasoningMode: cleaned}
}

func toExecutionGovernancePersonaEffective(profile sharedexec.EffectiveProfile) executionGovernancePersonaEffective {
	return executionGovernancePersonaEffective{
		SystemPrompt:           profile.SystemPrompt,
		ReasoningIterations:    profile.ReasoningIterations,
		ToolContinuationBudget: profile.ToolContinuationBudget,
		MaxOutputTokens:        profile.MaxOutputTokens,
		Temperature:            profile.Temperature,
		TopP:                   profile.TopP,
		ReasoningMode:          strings.TrimSpace(profile.ReasoningMode),
		PerToolSoftLimits:      profile.PerToolSoftLimits,
	}
}

func executionGovernanceOptionalTrimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return executionGovernanceOptionalTrimmedString(*value)
}

func executionGovernanceOptionalTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
