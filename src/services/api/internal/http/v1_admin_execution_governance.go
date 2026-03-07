package http

import (
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
	"github.com/jackc/pgx/v5/pgxpool"
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
	Limits              []sharedconfig.SettingInspection        `json:"limits"`
	AgentConfigDefaults executionGovernanceAgentConfigDefaults  `json:"agent_config_defaults"`
	AgentConfigs        []executionGovernanceAgentConfigSummary `json:"agent_configs"`
	Personas            []executionGovernancePersona            `json:"personas"`
}

type executionGovernanceAgentConfigDefaults struct {
	OrgDefault      *executionGovernanceAgentConfigSummary `json:"org_default"`
	PlatformDefault *executionGovernanceAgentConfigSummary `json:"platform_default"`
}

type executionGovernanceAgentConfigSummary struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Scope              string  `json:"scope"`
	IsDefault          bool    `json:"is_default"`
	ProjectID          *string `json:"project_id,omitempty"`
	PersonaID          *string `json:"persona_id,omitempty"`
	Model              *string `json:"model,omitempty"`
	MaxOutputTokens    *int    `json:"max_output_tokens,omitempty"`
	ReasoningMode      string  `json:"reasoning_mode,omitempty"`
	PromptCacheControl string  `json:"prompt_cache_control,omitempty"`
	ToolPolicy         string  `json:"tool_policy,omitempty"`
}

type executionGovernancePersona struct {
	ID                  string                              `json:"id"`
	Source              string                              `json:"source"`
	PersonaKey          string                              `json:"persona_key"`
	Version             string                              `json:"version"`
	DisplayName         string                              `json:"display_name"`
	PreferredCredential *string                             `json:"preferred_credential,omitempty"`
	AgentConfigName     *string                             `json:"agent_config_name,omitempty"`
	Requested           sharedexec.RequestedBudgets         `json:"requested"`
	Effective           executionGovernancePersonaEffective `json:"effective"`
}

type executionGovernancePersonaEffective struct {
	ResolvedAgentConfig    executionGovernanceResolvedAgentConfig `json:"resolved_agent_config"`
	ReasoningIterations    int                                    `json:"reasoning_iterations"`
	ToolContinuationBudget int                                    `json:"tool_continuation_budget"`
	MaxOutputTokens        *int                                   `json:"max_output_tokens,omitempty"`
	Temperature            *float64                               `json:"temperature,omitempty"`
	TopP                   *float64                               `json:"top_p,omitempty"`
	ReasoningMode          string                                 `json:"reasoning_mode,omitempty"`
	PerToolSoftLimits      sharedexec.PerToolSoftLimits           `json:"per_tool_soft_limits,omitempty"`
}

type executionGovernanceResolvedAgentConfig struct {
	Source string                                 `json:"source"`
	Config *executionGovernanceAgentConfigSummary `json:"config,omitempty"`
}

func adminExecutionGovernance(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	agentConfigsRepo *data.AgentConfigRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	registry *sharedconfig.Registry,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if registry == nil {
			registry = sharedconfig.DefaultRegistry()
		}

		orgID, ok := parseExecutionGovernanceOrgID(w, r, traceID)
		if !ok {
			return
		}

		store := sharedconfig.NewPGXStore(pool)
		scope := sharedconfig.Scope{OrgID: orgID}
		resp := executionGovernanceResponse{
			Limits:       make([]sharedconfig.SettingInspection, 0, len(executionGovernanceKeys)),
			AgentConfigs: []executionGovernanceAgentConfigSummary{},
			Personas:     []executionGovernancePersona{},
		}

		inspections := make(map[string]sharedconfig.SettingInspection, len(executionGovernanceKeys))
		for _, key := range executionGovernanceKeys {
			inspection, err := sharedconfig.Inspect(r.Context(), registry, store, key, scope)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			resp.Limits = append(resp.Limits, inspection)
			inspections[key] = inspection
		}

		if orgID == nil {
			writeJSON(w, traceID, nethttp.StatusOK, resp)
			return
		}

		platformLimits := executionGovernancePlatformLimits(inspections)
		var orgDefault *data.AgentConfig
		var platformDefault *data.AgentConfig
		if agentConfigsRepo != nil {
			var err error
			orgDefault, err = agentConfigsRepo.GetDefaultForOrg(r.Context(), *orgID)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			platformDefault, err = agentConfigsRepo.GetDefaultForPlatform(r.Context())
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			resp.AgentConfigDefaults = executionGovernanceAgentConfigDefaults{
				OrgDefault:      toExecutionGovernanceAgentConfigSummaryPtr(orgDefault),
				PlatformDefault: toExecutionGovernanceAgentConfigSummaryPtr(platformDefault),
			}
			configs, err := agentConfigsRepo.ListByOrg(r.Context(), *orgID)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			resp.AgentConfigs = make([]executionGovernanceAgentConfigSummary, 0, len(configs))
			for _, cfg := range configs {
				resp.AgentConfigs = append(resp.AgentConfigs, toExecutionGovernanceAgentConfigSummary(cfg))
			}
		}

		if personasRepo != nil {
			customPersonas, err := personasRepo.ListByOrg(r.Context(), *orgID)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			items, err := buildExecutionGovernancePersonas(
				r.Context(),
				*orgID,
				repoPersonas,
				customPersonas,
				agentConfigsRepo,
				orgDefault,
				platformDefault,
				platformLimits,
			)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			resp.Personas = items
		}

		writeJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func parseExecutionGovernanceOrgID(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if raw == "" {
		return nil, true
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org_id", traceID, nil)
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

func buildExecutionGovernancePersonas(
	ctx context.Context,
	orgID uuid.UUID,
	repoDefs []repopersonas.RepoPersona,
	customDefs []data.Persona,
	agentConfigsRepo *data.AgentConfigRepository,
	orgDefault *data.AgentConfig,
	platformDefault *data.AgentConfig,
	platformLimits sharedexec.PlatformLimits,
) ([]executionGovernancePersona, error) {
	items := make([]executionGovernancePersona, 0, len(customDefs)+len(repoDefs))
	shadowed := make(map[string]struct{}, len(customDefs))
	for _, persona := range customDefs {
		item, err := buildCustomExecutionGovernancePersona(ctx, orgID, persona, agentConfigsRepo, orgDefault, platformDefault, platformLimits)
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
		item, err := buildBuiltinExecutionGovernancePersona(ctx, orgID, persona, agentConfigsRepo, orgDefault, platformDefault, platformLimits)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func buildCustomExecutionGovernancePersona(
	ctx context.Context,
	orgID uuid.UUID,
	persona data.Persona,
	agentConfigsRepo *data.AgentConfigRepository,
	orgDefault *data.AgentConfig,
	platformDefault *data.AgentConfig,
	platformLimits sharedexec.PlatformLimits,
) (executionGovernancePersona, error) {
	requested, err := sharedexec.ParseRequestedBudgetsJSON(persona.BudgetsJSON)
	if err != nil {
		return executionGovernancePersona{}, err
	}
	resolvedConfig, source, err := resolveExecutionGovernanceAgentConfig(ctx, orgID, persona.AgentConfigName, agentConfigsRepo, orgDefault, platformDefault)
	if err != nil {
		return executionGovernancePersona{}, err
	}
	profile := sharedexec.ResolveEffectiveProfile(
		platformLimits,
		toExecutionAgentConfigProfile(resolvedConfig),
		&sharedexec.PersonaProfile{
			PreferredCredentialName: persona.PreferredCredential,
			ResolvedAgentConfigName: persona.AgentConfigName,
			Budgets:                 requested,
		},
	)
	return executionGovernancePersona{
		ID:                  persona.ID.String(),
		Source:              "custom",
		PersonaKey:          persona.PersonaKey,
		Version:             persona.Version,
		DisplayName:         persona.DisplayName,
		PreferredCredential: persona.PreferredCredential,
		AgentConfigName:     persona.AgentConfigName,
		Requested:           requested,
		Effective:           toExecutionGovernancePersonaEffective(profile, resolvedConfig, source),
	}, nil
}

func buildBuiltinExecutionGovernancePersona(
	ctx context.Context,
	orgID uuid.UUID,
	persona repopersonas.RepoPersona,
	agentConfigsRepo *data.AgentConfigRepository,
	orgDefault *data.AgentConfig,
	platformDefault *data.AgentConfig,
	platformLimits sharedexec.PlatformLimits,
) (executionGovernancePersona, error) {
	requested, err := sharedexec.ParseRequestedBudgetsMap(persona.Budgets)
	if err != nil {
		return executionGovernancePersona{}, err
	}
	agentConfigName := executionGovernanceOptionalTrimmedString(persona.AgentConfigName)
	preferredCredential := executionGovernanceOptionalTrimmedString(persona.PreferredCredential)
	resolvedConfig, source, err := resolveExecutionGovernanceAgentConfig(ctx, orgID, agentConfigName, agentConfigsRepo, orgDefault, platformDefault)
	if err != nil {
		return executionGovernancePersona{}, err
	}
	profile := sharedexec.ResolveEffectiveProfile(
		platformLimits,
		toExecutionAgentConfigProfile(resolvedConfig),
		&sharedexec.PersonaProfile{
			PreferredCredentialName: preferredCredential,
			ResolvedAgentConfigName: agentConfigName,
			Budgets:                 requested,
		},
	)
	return executionGovernancePersona{
		ID:                  "builtin:" + persona.ID + ":" + persona.Version,
		Source:              "builtin",
		PersonaKey:          persona.ID,
		Version:             persona.Version,
		DisplayName:         persona.Title,
		PreferredCredential: preferredCredential,
		AgentConfigName:     agentConfigName,
		Requested:           requested,
		Effective:           toExecutionGovernancePersonaEffective(profile, resolvedConfig, source),
	}, nil
}

func resolveExecutionGovernanceAgentConfig(
	ctx context.Context,
	orgID uuid.UUID,
	personaAgentConfigName *string,
	agentConfigsRepo *data.AgentConfigRepository,
	orgDefault *data.AgentConfig,
	platformDefault *data.AgentConfig,
) (*data.AgentConfig, string, error) {
	if agentConfigsRepo != nil && personaAgentConfigName != nil && strings.TrimSpace(*personaAgentConfigName) != "" {
		resolved, err := agentConfigsRepo.GetByNameForOrg(ctx, orgID, *personaAgentConfigName)
		if err != nil {
			return nil, "none", err
		}
		if resolved != nil {
			return resolved, "persona_binding", nil
		}
	}
	if orgDefault != nil {
		return orgDefault, "org_default", nil
	}
	if platformDefault != nil {
		return platformDefault, "platform_default", nil
	}
	return nil, "none", nil
}

func toExecutionGovernancePersonaEffective(
	profile sharedexec.EffectiveProfile,
	resolvedConfig *data.AgentConfig,
	source string,
) executionGovernancePersonaEffective {
	return executionGovernancePersonaEffective{
		ResolvedAgentConfig: executionGovernanceResolvedAgentConfig{
			Source: source,
			Config: toExecutionGovernanceAgentConfigSummaryPtr(resolvedConfig),
		},
		ReasoningIterations:    profile.ReasoningIterations,
		ToolContinuationBudget: profile.ToolContinuationBudget,
		MaxOutputTokens:        profile.MaxOutputTokens,
		Temperature:            profile.Temperature,
		TopP:                   profile.TopP,
		ReasoningMode:          profile.ReasoningMode,
		PerToolSoftLimits:      profile.PerToolSoftLimits,
	}
}

func toExecutionAgentConfigProfile(ac *data.AgentConfig) *sharedexec.AgentConfigProfile {
	if ac == nil {
		return nil
	}
	return &sharedexec.AgentConfigProfile{
		Name:            ac.Name,
		SystemPrompt:    ac.SystemPromptOverride,
		Temperature:     ac.Temperature,
		MaxOutputTokens: ac.MaxOutputTokens,
		TopP:            ac.TopP,
		ReasoningMode:   ac.ReasoningMode,
	}
}

func toExecutionGovernanceAgentConfigSummaryPtr(ac *data.AgentConfig) *executionGovernanceAgentConfigSummary {
	if ac == nil {
		return nil
	}
	summary := toExecutionGovernanceAgentConfigSummary(*ac)
	return &summary
}

func toExecutionGovernanceAgentConfigSummary(ac data.AgentConfig) executionGovernanceAgentConfigSummary {
	resp := executionGovernanceAgentConfigSummary{
		ID:                 ac.ID.String(),
		Name:               ac.Name,
		Scope:              ac.Scope,
		IsDefault:          ac.IsDefault,
		Model:              ac.Model,
		MaxOutputTokens:    ac.MaxOutputTokens,
		ReasoningMode:      ac.ReasoningMode,
		PromptCacheControl: ac.PromptCacheControl,
		ToolPolicy:         ac.ToolPolicy,
	}
	if ac.ProjectID != nil {
		value := ac.ProjectID.String()
		resp.ProjectID = &value
	}
	if ac.PersonaID != nil {
		value := ac.PersonaID.String()
		resp.PersonaID = &value
	}
	return resp
}

func executionGovernanceOptionalTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
