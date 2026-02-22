package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

// ─── Prompt Templates ────────────────────────────────────────────────────────

type promptTemplateResponse struct {
	ID          string   `json:"id"`
	OrgID       string   `json:"org_id"`
	Name        string   `json:"name"`
	Content     string   `json:"content"`
	Variables   []string `json:"variables"`
	IsDefault   bool     `json:"is_default"`
	Version     int      `json:"version"`
	PublishedAt *string  `json:"published_at,omitempty"`
	CreatedAt   string   `json:"created_at"`
}

type createPromptTemplateRequest struct {
	Name      string   `json:"name"`
	Content   string   `json:"content"`
	Variables []string `json:"variables"`
	IsDefault bool     `json:"is_default"`
}

type updatePromptTemplateRequest struct {
	Name      *string `json:"name"`
	Content   *string `json:"content"`
	IsDefault *bool   `json:"is_default"`
}

func promptTemplatesEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	templateRepo *data.PromptTemplateRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createPromptTemplate(w, r, authService, membershipRepo, templateRepo, apiKeysRepo)
		case nethttp.MethodGet:
			listPromptTemplates(w, r, authService, membershipRepo, templateRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func promptTemplateEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	templateRepo *data.PromptTemplateRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/prompt-templates/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		id, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid template id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getPromptTemplate(w, r, traceID, id, authService, membershipRepo, templateRepo, apiKeysRepo)
		case nethttp.MethodPatch:
			updatePromptTemplate(w, r, traceID, id, authService, membershipRepo, templateRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func createPromptTemplate(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	templateRepo *data.PromptTemplateRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if templateRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsManage, w, traceID) {
		return
	}

	var req createPromptTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must not be empty", traceID, nil)
		return
	}

	if req.Variables == nil {
		req.Variables = []string{}
	}

	pt, err := templateRepo.Create(r.Context(), actor.OrgID, req.Name, req.Content, req.Variables, req.IsDefault)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toPromptTemplateResponse(pt))
}

func listPromptTemplates(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	templateRepo *data.PromptTemplateRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if templateRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsRead, w, traceID) {
		return
	}

	templates, err := templateRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]promptTemplateResponse, 0, len(templates))
	for _, pt := range templates {
		resp = append(resp, toPromptTemplateResponse(pt))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func getPromptTemplate(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	templateRepo *data.PromptTemplateRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if templateRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsRead, w, traceID) {
		return
	}

	pt, err := templateRepo.GetByID(r.Context(), id)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if pt == nil || pt.OrgID != actor.OrgID {
		WriteError(w, nethttp.StatusNotFound, "agent_configs.not_found", "prompt template not found", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toPromptTemplateResponse(*pt))
}

func updatePromptTemplate(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	templateRepo *data.PromptTemplateRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if templateRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsManage, w, traceID) {
		return
	}

	var req updatePromptTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	if req.Name == nil && req.Content == nil && req.IsDefault == nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "no fields to update", traceID, nil)
		return
	}

	fields := data.PromptTemplateUpdateFields{}
	if req.Name != nil {
		fields.SetName = true
		fields.Name = strings.TrimSpace(*req.Name)
		if fields.Name == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must not be empty", traceID, nil)
			return
		}
	}
	if req.Content != nil {
		fields.SetContent = true
		fields.Content = *req.Content
	}
	if req.IsDefault != nil {
		fields.SetIsDefault = true
		fields.IsDefault = *req.IsDefault
	}

	// Update 内含 org_id 约束，原子完成所有权校验和更新
	updated, err := templateRepo.Update(r.Context(), id, actor.OrgID, fields)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		WriteError(w, nethttp.StatusNotFound, "agent_configs.not_found", "prompt template not found", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toPromptTemplateResponse(*updated))
}

func toPromptTemplateResponse(pt data.PromptTemplate) promptTemplateResponse {
	vars := pt.Variables
	if vars == nil {
		vars = []string{}
	}
	resp := promptTemplateResponse{
		ID:        pt.ID.String(),
		OrgID:     pt.OrgID.String(),
		Name:      pt.Name,
		Content:   pt.Content,
		Variables: vars,
		IsDefault: pt.IsDefault,
		Version:   pt.Version,
		CreatedAt: pt.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if pt.PublishedAt != nil {
		s := pt.PublishedAt.UTC().Format(time.RFC3339Nano)
		resp.PublishedAt = &s
	}
	return resp
}

// ─── Agent Configs ────────────────────────────────────────────────────────────

type agentConfigResponse struct {
	ID                     string         `json:"id"`
	OrgID                  string         `json:"org_id"`
	Name                   string         `json:"name"`
	SystemPromptTemplateID *string        `json:"system_prompt_template_id,omitempty"`
	SystemPromptOverride   *string        `json:"system_prompt_override,omitempty"`
	Model                  *string        `json:"model,omitempty"`
	Temperature            *float64       `json:"temperature,omitempty"`
	MaxOutputTokens        *int           `json:"max_output_tokens,omitempty"`
	TopP                   *float64       `json:"top_p,omitempty"`
	ContextWindowLimit     *int           `json:"context_window_limit,omitempty"`
	ToolPolicy             string         `json:"tool_policy"`
	ToolAllowlist          []string       `json:"tool_allowlist"`
	ToolDenylist           []string       `json:"tool_denylist"`
	ContentFilterLevel     string         `json:"content_filter_level"`
	SafetyRulesJSON        map[string]any `json:"safety_rules_json"`
	ProjectID              *string        `json:"project_id,omitempty"`
	SkillID                *string        `json:"skill_id,omitempty"`
	IsDefault              bool           `json:"is_default"`
	CreatedAt              string         `json:"created_at"`
}

type createAgentConfigRequest struct {
	Name                   string         `json:"name"`
	SystemPromptTemplateID *string        `json:"system_prompt_template_id"`
	SystemPromptOverride   *string        `json:"system_prompt_override"`
	Model                  *string        `json:"model"`
	Temperature            *float64       `json:"temperature"`
	MaxOutputTokens        *int           `json:"max_output_tokens"`
	TopP                   *float64       `json:"top_p"`
	ContextWindowLimit     *int           `json:"context_window_limit"`
	ToolPolicy             string         `json:"tool_policy"`
	ToolAllowlist          []string       `json:"tool_allowlist"`
	ToolDenylist           []string       `json:"tool_denylist"`
	ContentFilterLevel     string         `json:"content_filter_level"`
	SafetyRulesJSON        map[string]any `json:"safety_rules_json"`
	ProjectID              *string        `json:"project_id"`
	SkillID                *string        `json:"skill_id"`
	IsDefault              bool           `json:"is_default"`
}

type updateAgentConfigRequest struct {
	Name                   *string  `json:"name"`
	SystemPromptTemplateID *string  `json:"system_prompt_template_id"`
	SystemPromptOverride   *string  `json:"system_prompt_override"`
	Model                  *string  `json:"model"`
	Temperature            *float64 `json:"temperature"`
	MaxOutputTokens        *int     `json:"max_output_tokens"`
	TopP                   *float64 `json:"top_p"`
	ContextWindowLimit     *int     `json:"context_window_limit"`
	ToolPolicy             *string  `json:"tool_policy"`
	IsDefault              *bool    `json:"is_default"`
}

func agentConfigsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	agentConfigRepo *data.AgentConfigRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createAgentConfig(w, r, authService, membershipRepo, agentConfigRepo, apiKeysRepo)
		case nethttp.MethodGet:
			listAgentConfigs(w, r, authService, membershipRepo, agentConfigRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func agentConfigEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	agentConfigRepo *data.AgentConfigRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/agent-configs/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		id, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid agent config id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getAgentConfig(w, r, traceID, id, authService, membershipRepo, agentConfigRepo, apiKeysRepo)
		case nethttp.MethodPatch:
			updateAgentConfig(w, r, traceID, id, authService, membershipRepo, agentConfigRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func createAgentConfig(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	agentConfigRepo *data.AgentConfigRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if agentConfigRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsManage, w, traceID) {
		return
	}

	var req createAgentConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must not be empty", traceID, nil)
		return
	}

	if req.ToolPolicy != "" && req.ToolPolicy != "allowlist" && req.ToolPolicy != "denylist" && req.ToolPolicy != "none" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "tool_policy must be allowlist, denylist, or none", traceID, nil)
		return
	}

	createReq, err := toCreateAgentConfigRequest(req)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	ac, err := agentConfigRepo.Create(r.Context(), actor.OrgID, createReq)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toAgentConfigResponse(ac))
}

func listAgentConfigs(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	agentConfigRepo *data.AgentConfigRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if agentConfigRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsRead, w, traceID) {
		return
	}

	configs, err := agentConfigRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]agentConfigResponse, 0, len(configs))
	for _, ac := range configs {
		resp = append(resp, toAgentConfigResponse(ac))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func getAgentConfig(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	agentConfigRepo *data.AgentConfigRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if agentConfigRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsRead, w, traceID) {
		return
	}

	ac, err := agentConfigRepo.GetByID(r.Context(), id)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ac == nil || ac.OrgID != actor.OrgID {
		WriteError(w, nethttp.StatusNotFound, "agent_configs.not_found", "agent config not found", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toAgentConfigResponse(*ac))
}

func updateAgentConfig(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	agentConfigRepo *data.AgentConfigRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if agentConfigRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataAgentConfigsManage, w, traceID) {
		return
	}

	var req updateAgentConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	if req.Name == nil && req.SystemPromptTemplateID == nil && req.SystemPromptOverride == nil &&
		req.Model == nil && req.Temperature == nil && req.MaxOutputTokens == nil &&
		req.TopP == nil && req.ContextWindowLimit == nil && req.ToolPolicy == nil && req.IsDefault == nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "no fields to update", traceID, nil)
		return
	}

	if req.ToolPolicy != nil && *req.ToolPolicy != "allowlist" && *req.ToolPolicy != "denylist" && *req.ToolPolicy != "none" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "tool_policy must be allowlist, denylist, or none", traceID, nil)
		return
	}

	fields := data.AgentConfigUpdateFields{}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must not be empty", traceID, nil)
			return
		}
		fields.SetName = true
		fields.Name = n
	}
	if req.SystemPromptTemplateID != nil {
		fields.SetSystemPromptTemplateID = true
		if *req.SystemPromptTemplateID != "" {
			parsed, err := uuid.Parse(*req.SystemPromptTemplateID)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid system_prompt_template_id", traceID, nil)
				return
			}
			fields.SystemPromptTemplateID = &parsed
		}
	}
	if req.SystemPromptOverride != nil {
		fields.SetSystemPromptOverride = true
		fields.SystemPromptOverride = req.SystemPromptOverride
	}
	if req.Model != nil {
		fields.SetModel = true
		fields.Model = req.Model
	}
	if req.Temperature != nil {
		fields.SetTemperature = true
		fields.Temperature = req.Temperature
	}
	if req.MaxOutputTokens != nil {
		fields.SetMaxOutputTokens = true
		fields.MaxOutputTokens = req.MaxOutputTokens
	}
	if req.TopP != nil {
		fields.SetTopP = true
		fields.TopP = req.TopP
	}
	if req.ContextWindowLimit != nil {
		fields.SetContextWindowLimit = true
		fields.ContextWindowLimit = req.ContextWindowLimit
	}
	if req.ToolPolicy != nil {
		fields.SetToolPolicy = true
		fields.ToolPolicy = *req.ToolPolicy
	}
	if req.IsDefault != nil {
		fields.SetIsDefault = true
		fields.IsDefault = *req.IsDefault
	}

	// Update 内含 org_id 约束，原子完成所有权校验和更新
	updated, err := agentConfigRepo.Update(r.Context(), id, actor.OrgID, fields)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		WriteError(w, nethttp.StatusNotFound, "agent_configs.not_found", "agent config not found", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toAgentConfigResponse(*updated))
}

func toCreateAgentConfigRequest(req createAgentConfigRequest) (data.CreateAgentConfigRequest, error) {
	createReq := data.CreateAgentConfigRequest{
		Name:                 req.Name,
		SystemPromptOverride: req.SystemPromptOverride,
		Model:                req.Model,
		Temperature:          req.Temperature,
		MaxOutputTokens:      req.MaxOutputTokens,
		TopP:                 req.TopP,
		ContextWindowLimit:   req.ContextWindowLimit,
		ToolPolicy:           req.ToolPolicy,
		ToolAllowlist:        req.ToolAllowlist,
		ToolDenylist:         req.ToolDenylist,
		ContentFilterLevel:   req.ContentFilterLevel,
		SafetyRulesJSON:      req.SafetyRulesJSON,
		IsDefault:            req.IsDefault,
	}

	if req.SystemPromptTemplateID != nil && *req.SystemPromptTemplateID != "" {
		parsed, err := uuid.Parse(*req.SystemPromptTemplateID)
		if err != nil {
			return data.CreateAgentConfigRequest{}, err
		}
		createReq.SystemPromptTemplateID = &parsed
	}
	if req.ProjectID != nil && *req.ProjectID != "" {
		parsed, err := uuid.Parse(*req.ProjectID)
		if err != nil {
			return data.CreateAgentConfigRequest{}, err
		}
		createReq.ProjectID = &parsed
	}
	if req.SkillID != nil && *req.SkillID != "" {
		parsed, err := uuid.Parse(*req.SkillID)
		if err != nil {
			return data.CreateAgentConfigRequest{}, err
		}
		createReq.SkillID = &parsed
	}
	return createReq, nil
}

func toAgentConfigResponse(ac data.AgentConfig) agentConfigResponse {
	allowlist := ac.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	denylist := ac.ToolDenylist
	if denylist == nil {
		denylist = []string{}
	}
	safetyRules := ac.SafetyRulesJSON
	if safetyRules == nil {
		safetyRules = map[string]any{}
	}

	resp := agentConfigResponse{
		ID:                   ac.ID.String(),
		OrgID:                ac.OrgID.String(),
		Name:                 ac.Name,
		SystemPromptOverride: ac.SystemPromptOverride,
		Model:                ac.Model,
		Temperature:          ac.Temperature,
		MaxOutputTokens:      ac.MaxOutputTokens,
		TopP:                 ac.TopP,
		ContextWindowLimit:   ac.ContextWindowLimit,
		ToolPolicy:           ac.ToolPolicy,
		ToolAllowlist:        allowlist,
		ToolDenylist:         denylist,
		ContentFilterLevel:   ac.ContentFilterLevel,
		SafetyRulesJSON:      safetyRules,
		IsDefault:            ac.IsDefault,
		CreatedAt:            ac.CreatedAt.UTC().Format(time.RFC3339Nano),
	}

	if ac.SystemPromptTemplateID != nil {
		s := ac.SystemPromptTemplateID.String()
		resp.SystemPromptTemplateID = &s
	}
	if ac.ProjectID != nil {
		s := ac.ProjectID.String()
		resp.ProjectID = &s
	}
	if ac.SkillID != nil {
		s := ac.SkillID.String()
		resp.SkillID = &s
	}
	return resp
}
