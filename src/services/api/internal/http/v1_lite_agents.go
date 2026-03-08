package http

import (
	"encoding/json"
	"errors"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/personas"

	"github.com/google/uuid"
)

type liteAgentResponse struct {
	ID              string          `json:"id"`
	PersonaKey      string          `json:"persona_key"`
	DisplayName     string          `json:"display_name"`
	Description     *string         `json:"description,omitempty"`
	PromptMD        string          `json:"prompt_md"`
	Model           *string         `json:"model,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxOutputTokens *int            `json:"max_output_tokens,omitempty"`
	ReasoningMode   string          `json:"reasoning_mode"`
	ToolPolicy      string          `json:"tool_policy"`
	ToolAllowlist   []string        `json:"tool_allowlist"`
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
	ToolAllowlist          []string `json:"tool_allowlist"`
	ExecutorType           string   `json:"executor_type"`
}

type patchLiteAgentRequest struct {
	Name            *string   `json:"name"`
	PromptMD        *string   `json:"prompt_md"`
	Model           *string   `json:"model"`
	Temperature     *float64  `json:"temperature"`
	MaxOutputTokens *int      `json:"max_output_tokens"`
	ReasoningMode   *string   `json:"reasoning_mode"`
	ToolAllowlist   *[]string `json:"tool_allowlist"`
	IsActive        *bool     `json:"is_active"`
}

func liteAgentsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []personas.RepoPersona,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			listLiteAgents(w, r, authService, membershipRepo, personasRepo, repoPersonas)
		case nethttp.MethodPost:
			createLiteAgent(w, r, authService, membershipRepo, personasRepo, repoPersonas)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func liteAgentEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		tail := strings.TrimPrefix(r.URL.Path, "/v1/lite/agents/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}
		personaID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid id", traceID, nil)
			return
		}
		switch r.Method {
		case nethttp.MethodPatch:
			patchLiteAgent(w, r, traceID, personaID, authService, membershipRepo, personasRepo)
		case nethttp.MethodDelete:
			deleteLiteAgent(w, r, traceID, personaID, authService, membershipRepo, personasRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func listLiteAgents(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []personas.RepoPersona,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	dbPersonas, err := personasRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
		resp = append(resp, toLiteAgentFromRepo(rp))
	}

	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func createLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []personas.RepoPersona,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
		return
	}

	var req createLiteAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.PromptMD = strings.TrimSpace(req.PromptMD)
	req.CopyFromRepoPersonaKey = strings.TrimSpace(req.CopyFromRepoPersonaKey)
	if req.CopyFromRepoPersonaKey != "" {
		repoPersona, ok := findRepoPersonaByKey(repoPersonas, req.CopyFromRepoPersonaKey)
		if !ok {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "copy_from_repo_persona_key is invalid", traceID, nil)
			return
		}

		persona, err := materializeRepoPersonaForLiteAgent(r.Context(), personasRepo, actor.OrgID, *repoPersona, req)
		if err != nil {
			var conflict data.PersonaConflictError
			if errors.As(err, &conflict) {
				WriteError(w, nethttp.StatusConflict, "lite_agents.conflict", "agent with this key and version already exists", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		writeJSON(w, traceID, nethttp.StatusCreated, toLiteAgentFromDB(persona))
		return
	}
	if req.Name == "" || req.PromptMD == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name and prompt_md are required", traceID, nil)
		return
	}

	persona, err := personasRepo.Create(
		r.Context(),
		actor.OrgID,
		slugify(req.Name),
		"1.0",
		req.Name,
		nil,
		req.PromptMD,
		req.ToolAllowlist,
		nil,
		mergeLiteAgentBudgets(nil, req.Temperature, req.MaxOutputTokens),
		nil,
		req.Model,
		req.ReasoningMode,
		"none",
		req.ExecutorType,
		nil,
	)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	writeJSON(w, traceID, nethttp.StatusCreated, toLiteAgentFromDB(persona))
}

func patchLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
		return
	}

	existing, err := personasRepo.GetByID(r.Context(), actor.OrgID, personaID)
	if err != nil || existing == nil {
		WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}

	var req patchLiteAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	patch := data.PersonaPatch{
		DisplayName:        req.Name,
		PromptMD:           req.PromptMD,
		BudgetsJSON:        mergeLiteAgentBudgets(existing.BudgetsJSON, req.Temperature, req.MaxOutputTokens),
		IsActive:           req.IsActive,
		Model:              req.Model,
		ReasoningMode:      req.ReasoningMode,
		PromptCacheControl: ptrString("none"),
	}
	if req.ToolAllowlist != nil {
		patch.ToolAllowlist = *req.ToolAllowlist
	}

	updated, err := personasRepo.Patch(r.Context(), actor.OrgID, personaID, patch)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, toLiteAgentFromDB(*updated))
}

func deleteLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
		return
	}
	deleted, err := personasRepo.Delete(r.Context(), actor.OrgID, personaID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !deleted {
		WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toLiteAgentFromDB(p data.Persona) liteAgentResponse {
	allowlist := p.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
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
	}
	return liteAgentResponse{
		ID:              p.ID.String(),
		PersonaKey:      p.PersonaKey,
		DisplayName:     p.DisplayName,
		Description:     p.Description,
		PromptMD:        p.PromptMD,
		Model:           optionalLiteTrimmedStringPtr(p.Model),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ReasoningMode:   reasoningMode,
		ToolPolicy:      toolPolicy,
		ToolAllowlist:   allowlist,
		IsActive:        p.IsActive,
		ExecutorType:    executorType,
		BudgetsJSON:     budgets,
		Source:          "db",
		CreatedAt:       p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func toLiteAgentFromRepo(rp personas.RepoPersona) liteAgentResponse {
	allowlist := rp.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
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
	}
	return liteAgentResponse{
		ID:              rp.ID,
		PersonaKey:      rp.ID,
		DisplayName:     rp.Title,
		Description:     descPtr,
		PromptMD:        rp.PromptMD,
		Model:           optionalLiteTrimmedString(rp.Model),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ReasoningMode:   reasoningMode,
		ToolPolicy:      toolPolicy,
		ToolAllowlist:   allowlist,
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
