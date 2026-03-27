package catalogapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/personas"

	"github.com/google/uuid"

	nethttp "net/http"
)

type liteAgentResponse struct {
	ID              string          `json:"id"`
	Scope           string          `json:"scope"`
	PersonaKey      string          `json:"persona_key"`
	DisplayName     string          `json:"display_name"`
	Description     *string         `json:"description,omitempty"`
	PromptMD        string          `json:"prompt_md"`
	Model           *string         `json:"model,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxOutputTokens *int            `json:"max_output_tokens,omitempty"`
	ReasoningMode   string          `json:"reasoning_mode"`
	StreamThinking  bool            `json:"stream_thinking"`
	ToolPolicy      string          `json:"tool_policy"`
	ToolAllowlist   []string        `json:"tool_allowlist"`
	ToolDenylist    []string        `json:"tool_denylist"`
	CoreTools       []string        `json:"core_tools"`
	IsActive        bool            `json:"is_active"`
	ExecutorType    string          `json:"executor_type"`
	BudgetsJSON     json.RawMessage `json:"budgets"`
	Source          string          `json:"source"`
	CreatedAt       string          `json:"created_at"`
}

type createLiteAgentRequest struct {
	CopyFromRepoPersonaKey string   `json:"copy_from_repo_persona_key"`
	Name                   string   `json:"name"`
	PromptMD               string   `json:"prompt_md"`
	Model                  *string  `json:"model"`
	Temperature            *float64 `json:"temperature"`
	MaxOutputTokens        *int     `json:"max_output_tokens"`
	ReasoningMode          string   `json:"reasoning_mode"`
	StreamThinking         *bool    `json:"stream_thinking"`
	ToolAllowlist          []string `json:"tool_allowlist"`
	ToolDenylist           []string `json:"tool_denylist"`
	ExecutorType           string   `json:"executor_type"`
	Scope                  string   `json:"scope"`
}

type patchLiteAgentRequest struct {
	Name            *string   `json:"name"`
	PromptMD        *string   `json:"prompt_md"`
	Model           *string   `json:"model"`
	Temperature     *float64  `json:"temperature"`
	MaxOutputTokens *int      `json:"max_output_tokens"`
	ReasoningMode   *string   `json:"reasoning_mode"`
	StreamThinking  *bool     `json:"stream_thinking"`
	ToolAllowlist   *[]string `json:"tool_allowlist"`
	ToolDenylist    *[]string `json:"tool_denylist"`
	CoreTools       *[]string `json:"core_tools"`
	IsActive        *bool     `json:"is_active"`
}

func liteAgentsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []personas.RepoPersona,
	syncTrigger personaSyncTrigger,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			listLiteAgents(w, r, authService, membershipRepo, personasRepo, repoPersonas)
		case nethttp.MethodPost:
			createLiteAgent(w, r, authService, membershipRepo, personasRepo, repoPersonas, syncTrigger)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func liteAgentEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	syncTrigger personaSyncTrigger,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		tail := strings.TrimPrefix(r.URL.Path, "/v1/lite/agents/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}
		personaID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid id", traceID, nil)
			return
		}
		switch r.Method {
		case nethttp.MethodPatch:
			patchLiteAgent(w, r, traceID, personaID, authService, membershipRepo, personasRepo, syncTrigger)
		case nethttp.MethodDelete:
			deleteLiteAgent(w, r, traceID, personaID, authService, membershipRepo, personasRepo, syncTrigger)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func listLiteAgents(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []personas.RepoPersona,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, r.URL.Query().Get("scope"), false, false)
	if !ok {
		return
	}
	scopeID, ok := resolveLiteAgentScopeID(r.Context(), w, traceID, actor, scope, personasRepo)
	if !ok {
		return
	}

	dbPersonas, err := personasRepo.ListByScope(r.Context(), scopeID, scope)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	dbPersonaKeys := make(map[string]bool, len(dbPersonas))
	resp := make([]liteAgentResponse, 0, len(dbPersonas)+len(repoPersonas))
	for _, p := range dbPersonas {
		dbPersonaKeys[p.PersonaKey] = true
		resp = append(resp, toLiteAgentFromDB(p))
	}
	for _, rp := range repoPersonas {
		if dbPersonaKeys[rp.ID] {
			continue
		}
		resp = append(resp, toLiteAgentFromRepo(rp, scope))
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func createLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []personas.RepoPersona,
	syncTrigger personaSyncTrigger,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	var req createLiteAgentRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, req.Scope, true, true)
	if !ok {
		return
	}
	scopeID, ok := resolveLiteAgentScopeID(r.Context(), w, traceID, actor, scope, personasRepo)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.PromptMD = strings.TrimSpace(req.PromptMD)
	req.CopyFromRepoPersonaKey = strings.TrimSpace(req.CopyFromRepoPersonaKey)
	if req.CopyFromRepoPersonaKey != "" {
		repoPersona, exists := findRepoPersonaByKey(repoPersonas, req.CopyFromRepoPersonaKey)
		if !exists {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "copy_from_repo_persona_key is invalid", traceID, nil)
			return
		}

		persona, err := materializeRepoPersonaForLiteAgent(r.Context(), personasRepo, scopeID, scope, *repoPersona, req)
		if err != nil {
			var conflict data.PersonaConflictError
			if errors.As(err, &conflict) {
				httpkit.WriteError(w, nethttp.StatusConflict, "lite_agents.conflict", "agent with this key and version already exists", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toLiteAgentFromDB(persona))
		notifyPersonaSync(syncTrigger)
		return
	}
	if req.Name == "" || req.PromptMD == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name and prompt_md are required", traceID, nil)
		return
	}

	persona, err := personasRepo.CreateInScope(
		r.Context(),
		scopeID,
		scope,
		slugify(req.Name),
		"1.0",
		req.Name,
		nil,
		req.PromptMD,
		req.ToolAllowlist,
		req.ToolDenylist,
		mergeLiteAgentBudgets(nil, req.Temperature, req.MaxOutputTokens),
		nil,
		nil,
		nil,
		req.Model,
		req.ReasoningMode,
		data.NormalizePersonaStreamThinkingPtr(req.StreamThinking),
		"none",
		req.ExecutorType,
		nil,
	)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toLiteAgentFromDB(persona))
	notifyPersonaSync(syncTrigger)
}

func patchLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	syncTrigger personaSyncTrigger,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, r.URL.Query().Get("scope"), false, true)
	if !ok {
		return
	}
	scopeID, ok := resolveLiteAgentScopeID(r.Context(), w, traceID, actor, scope, personasRepo)
	if !ok {
		return
	}

	existing, err := personasRepo.GetByIDInScope(r.Context(), scopeID, personaID, scope)
	if err != nil || existing == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}

	var req patchLiteAgentRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	patch := data.PersonaPatch{
		DisplayName:        req.Name,
		PromptMD:           req.PromptMD,
		BudgetsJSON:        mergeLiteAgentBudgets(existing.BudgetsJSON, req.Temperature, req.MaxOutputTokens),
		IsActive:           req.IsActive,
		Model:              req.Model,
		ReasoningMode:      req.ReasoningMode,
		StreamThinking:     req.StreamThinking,
		PromptCacheControl: ptrString("none"),
	}
	if req.ToolAllowlist != nil {
		patch.ToolAllowlist = *req.ToolAllowlist
	}
	if req.ToolDenylist != nil {
		patch.ToolDenylist = *req.ToolDenylist
	}
	if req.CoreTools != nil {
		patch.CoreTools = *req.CoreTools
	}

	updated, err := personasRepo.PatchInScope(r.Context(), scopeID, personaID, scope, patch)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toLiteAgentFromDB(*updated))
	notifyPersonaSync(syncTrigger)
}

func deleteLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	syncTrigger personaSyncTrigger,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, r.URL.Query().Get("scope"), false, true)
	if !ok {
		return
	}
	scopeID, ok := resolveLiteAgentScopeID(r.Context(), w, traceID, actor, scope, personasRepo)
	if !ok {
		return
	}
	deleted, err := personasRepo.DeleteInScope(r.Context(), scopeID, personaID, scope)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !deleted {
		httpkit.WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
	notifyPersonaSync(syncTrigger)
}

func toLiteAgentFromDB(p data.Persona) liteAgentResponse {
	allowlist := p.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	denylist := p.ToolDenylist
	if denylist == nil {
		denylist = []string{}
	}
	budgets := p.BudgetsJSON
	if len(budgets) == 0 {
		budgets = json.RawMessage("{}")
	}
	executorType := strings.TrimSpace(p.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}
	temperature, maxOutputTokens := extractLiteAgentBudgetValues(budgets)
	reasoningMode := strings.TrimSpace(p.ReasoningMode)
	if reasoningMode == "" {
		reasoningMode = "auto"
	}
	toolPolicy := "none"
	if len(allowlist) > 0 {
		toolPolicy = "allowlist"
	} else if len(denylist) > 0 {
		toolPolicy = "denylist"
	}
	return liteAgentResponse{
		ID:              p.ID.String(),
		Scope:           personaScopeFromProjectID(p.ProjectID),
		PersonaKey:      p.PersonaKey,
		DisplayName:     p.DisplayName,
		Description:     p.Description,
		PromptMD:        p.PromptMD,
		Model:           optionalLiteTrimmedStringPtr(p.Model),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ReasoningMode:   reasoningMode,
		StreamThinking:  p.StreamThinking,
		ToolPolicy:      toolPolicy,
		ToolAllowlist:   allowlist,
		ToolDenylist:    denylist,
		CoreTools:       orEmptyStringSlice(p.CoreTools),
		IsActive:        p.IsActive,
		ExecutorType:    executorType,
		BudgetsJSON:     budgets,
		Source:          "db",
		CreatedAt:       p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func resolveLiteAgentScopeID(
	ctx context.Context,
	w nethttp.ResponseWriter,
	traceID string,
	actor *httpkit.Actor,
	scope string,
	personasRepo *data.PersonasRepository,
) (uuid.UUID, bool) {
	if scope != data.PersonaScopeProject {
		return uuid.Nil, true
	}
	if personasRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return uuid.Nil, false
	}
	projectID, err := personasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, actor.AccountID, actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return uuid.Nil, false
	}
	return projectID, true
}

func toLiteAgentFromRepo(rp personas.RepoPersona, scope string) liteAgentResponse {
	allowlist := rp.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	denylist := rp.ToolDenylist
	if denylist == nil {
		denylist = []string{}
	}
	budgets := json.RawMessage("{}")
	if rp.Budgets != nil {
		if b, err := json.Marshal(rp.Budgets); err == nil {
			budgets = b
		}
	}
	executorType := strings.TrimSpace(rp.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}
	desc := strings.TrimSpace(rp.Description)
	var descPtr *string
	if desc != "" {
		descPtr = &desc
	}
	temperature, maxOutputTokens := extractLiteAgentBudgetValues(budgets)
	reasoningMode := strings.TrimSpace(rp.ReasoningMode)
	if reasoningMode == "" {
		reasoningMode = "auto"
	}
	toolPolicy := "none"
	if len(allowlist) > 0 {
		toolPolicy = "allowlist"
	} else if len(denylist) > 0 {
		toolPolicy = "denylist"
	}
	return liteAgentResponse{
		ID:              rp.ID,
		Scope:           personaScopeFromScope(scope),
		PersonaKey:      rp.ID,
		DisplayName:     rp.Title,
		Description:     descPtr,
		PromptMD:        rp.PromptMD,
		Model:           optionalLiteTrimmedString(rp.Model),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ReasoningMode:   reasoningMode,
		StreamThinking:  data.NormalizePersonaStreamThinkingPtr(rp.StreamThinking),
		ToolPolicy:      toolPolicy,
		ToolAllowlist:   allowlist,
		ToolDenylist:    denylist,
		CoreTools:       orEmptyStringSlice(rp.CoreTools),
		IsActive:        true,
		ExecutorType:    executorType,
		BudgetsJSON:     budgets,
		Source:          "repo",
		CreatedAt:       "",
	}
}

func mergeLiteAgentBudgets(base json.RawMessage, temperature *float64, maxOutputTokens *int) json.RawMessage {
	payload := map[string]any{}
	if len(base) > 0 {
		_ = json.Unmarshal(base, &payload)
	}
	if temperature != nil {
		payload["temperature"] = *temperature
	}
	if maxOutputTokens != nil {
		payload["max_output_tokens"] = *maxOutputTokens
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

func extractLiteAgentBudgetValues(raw json.RawMessage) (*float64, *int) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil
	}
	var temperature *float64
	if value, ok := payload["temperature"].(float64); ok {
		temperature = &value
	}
	var maxOutputTokens *int
	switch value := payload["max_output_tokens"].(type) {
	case float64:
		converted := int(value)
		maxOutputTokens = &converted
	case int:
		maxOutputTokens = &value
	}
	return temperature, maxOutputTokens
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteRune('-')
		}
	}
	result := b.String()
	if result == "" {
		result = "agent"
	}
	return result
}

func ptrString(s string) *string {
	return &s
}

func optionalLiteTrimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return optionalLiteTrimmedString(*value)
}

func optionalLiteTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
