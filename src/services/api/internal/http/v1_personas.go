package http

import (
	"encoding/json"
	"errors"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"

	"github.com/google/uuid"
)

type createPersonaRequest struct {
	CopyFromRepoPersonaKey string          `json:"copy_from_repo_persona_key"`
	PersonaKey             string          `json:"persona_key"`
	Version                string          `json:"version"`
	DisplayName            string          `json:"display_name"`
	Description            *string         `json:"description"`
	PromptMD               string          `json:"prompt_md"`
	ToolAllowlist          []string        `json:"tool_allowlist"`
	ToolDenylist           []string        `json:"tool_denylist"`
	BudgetsJSON            json.RawMessage `json:"budgets"`
	PreferredCredential    *string         `json:"preferred_credential"`
	Model                  *string         `json:"model"`
	ReasoningMode          string          `json:"reasoning_mode"`
	PromptCacheControl     string          `json:"prompt_cache_control"`
	ExecutorType           string          `json:"executor_type"`
	ExecutorConfigJSON     json.RawMessage `json:"executor_config"`
}

type patchPersonaRequest struct {
	DisplayName         *string         `json:"display_name"`
	Description         *string         `json:"description"`
	PromptMD            *string         `json:"prompt_md"`
	ToolAllowlist       []string        `json:"tool_allowlist"`
	ToolDenylist        []string        `json:"tool_denylist"`
	BudgetsJSON         json.RawMessage `json:"budgets"`
	IsActive            *bool           `json:"is_active"`
	PreferredCredential *string         `json:"preferred_credential"`
	Model               *string         `json:"model"`
	ReasoningMode       *string         `json:"reasoning_mode"`
	PromptCacheControl  *string         `json:"prompt_cache_control"`
	ExecutorType        *string         `json:"executor_type"`
	ExecutorConfigJSON  json.RawMessage `json:"executor_config"`
}

type personaResponse struct {
	ID                  string          `json:"id"`
	OrgID               *string         `json:"org_id"`
	PersonaKey          string          `json:"persona_key"`
	Version             string          `json:"version"`
	DisplayName         string          `json:"display_name"`
	Description         *string         `json:"description,omitempty"`
	UserSelectable      bool            `json:"user_selectable"`
	SelectorName        *string         `json:"selector_name,omitempty"`
	SelectorOrder       *int            `json:"selector_order,omitempty"`
	PromptMD            string          `json:"prompt_md"`
	ToolAllowlist       []string        `json:"tool_allowlist"`
	ToolDenylist        []string        `json:"tool_denylist"`
	BudgetsJSON         json.RawMessage `json:"budgets"`
	IsActive            bool            `json:"is_active"`
	CreatedAt           string          `json:"created_at"`
	PreferredCredential *string         `json:"preferred_credential,omitempty"`
	Model               *string         `json:"model,omitempty"`
	ReasoningMode       string          `json:"reasoning_mode"`
	PromptCacheControl  string          `json:"prompt_cache_control"`
	ExecutorType        string          `json:"executor_type"`
	ExecutorConfigJSON  json.RawMessage `json:"executor_config"`
	Source              string          `json:"source"`
}

func personasEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodPost:
			createPersona(w, r, traceID, authService, membershipRepo, personasRepo, repoPersonas)
		case nethttp.MethodGet:
			listPersonas(w, r, traceID, authService, membershipRepo, personasRepo, repoPersonas)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func personaEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/personas/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		personaID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPatch:
			patchPersona(w, r, traceID, personaID, authService, membershipRepo, personasRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func createPersona(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if personasRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
		return
	}

	var req createPersonaRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.PersonaKey = strings.TrimSpace(req.PersonaKey)
	req.Version = strings.TrimSpace(req.Version)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.PromptMD = strings.TrimSpace(req.PromptMD)
	req.CopyFromRepoPersonaKey = strings.TrimSpace(req.CopyFromRepoPersonaKey)

	if req.CopyFromRepoPersonaKey != "" {
		repoPersona, ok := findRepoPersonaByKey(repoPersonas, req.CopyFromRepoPersonaKey)
		if !ok {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "copy_from_repo_persona_key is invalid", traceID, nil)
			return
		}
		if req.PersonaKey != "" && req.PersonaKey != repoPersona.ID {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "persona_key must match repo persona key", traceID, nil)
			return
		}
		if req.Version != "" && req.Version != repoPersona.Version {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "version must match repo persona version", traceID, nil)
			return
		}

		persona, err := materializeRepoPersonaForCreate(r.Context(), personasRepo, actor.OrgID, *repoPersona, req)
		if err != nil {
			var conflict data.PersonaConflictError
			if errors.As(err, &conflict) {
				WriteError(w, nethttp.StatusConflict, "personas.conflict", "persona with this key and version already exists", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusCreated, toPersonaResponse(persona))
		return
	}

	if req.PersonaKey == "" || req.Version == "" || req.DisplayName == "" || req.PromptMD == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "persona_key, version, display_name, and prompt_md are required", traceID, nil)
		return
	}

	persona, err := personasRepo.Create(
		r.Context(),
		actor.OrgID,
		req.PersonaKey,
		req.Version,
		req.DisplayName,
		req.Description,
		req.PromptMD,
		req.ToolAllowlist,
		req.ToolDenylist,
		req.BudgetsJSON,
		req.PreferredCredential,
		req.Model,
		req.ReasoningMode,
		req.PromptCacheControl,
		req.ExecutorType,
		req.ExecutorConfigJSON,
	)
	if err != nil {
		var conflict data.PersonaConflictError
		if errors.As(err, &conflict) {
			WriteError(w, nethttp.StatusConflict, "personas.conflict", "persona with this key and version already exists", traceID, nil)
			return
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toPersonaResponse(persona))
}

func listPersonas(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if personasRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
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

	dbPersonaKeys := make(map[string]struct{}, len(dbPersonas))
	resp := make([]personaResponse, 0, len(dbPersonas)+len(repoPersonas))
	for _, persona := range dbPersonas {
		dbPersonaKeys[persona.PersonaKey] = struct{}{}
		resp = append(resp, toPersonaResponse(persona))
	}
	for _, persona := range repoPersonas {
		if _, exists := dbPersonaKeys[persona.ID]; exists {
			continue
		}
		resp = append(resp, toBuiltinPersonaResponse(persona))
	}

	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func patchPersona(
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
	if personasRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
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
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		WriteError(w, nethttp.StatusNotFound, "personas.not_found", "persona not found", traceID, nil)
		return
	}

	var req patchPersonaRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	patch := data.PersonaPatch{
		DisplayName:         req.DisplayName,
		Description:         req.Description,
		PromptMD:            req.PromptMD,
		ToolAllowlist:       req.ToolAllowlist,
		ToolDenylist:        req.ToolDenylist,
		BudgetsJSON:         req.BudgetsJSON,
		IsActive:            req.IsActive,
		PreferredCredential: req.PreferredCredential,
		Model:               req.Model,
		ReasoningMode:       req.ReasoningMode,
		PromptCacheControl:  req.PromptCacheControl,
		ExecutorType:        req.ExecutorType,
		ExecutorConfigJSON:  req.ExecutorConfigJSON,
	}

	updated, err := personasRepo.Patch(r.Context(), actor.OrgID, personaID, patch)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		WriteError(w, nethttp.StatusNotFound, "personas.not_found", "persona not found", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toPersonaResponse(*updated))
}

func toPersonaResponse(s data.Persona) personaResponse {
	allowlist := s.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	denylist := s.ToolDenylist
	if denylist == nil {
		denylist = []string{}
	}
	budgets := s.BudgetsJSON
	if len(budgets) == 0 {
		budgets = json.RawMessage("{}")
	}
	executorConfig := s.ExecutorConfigJSON
	if len(executorConfig) == 0 {
		executorConfig = json.RawMessage("{}")
	}
	executorType := strings.TrimSpace(s.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}
	reasoningMode := strings.TrimSpace(s.ReasoningMode)
	if reasoningMode == "" {
		reasoningMode = "auto"
	}
	promptCacheControl := strings.TrimSpace(s.PromptCacheControl)
	if promptCacheControl == "" {
		promptCacheControl = "none"
	}

	var orgIDStr *string
	if s.OrgID != nil {
		value := s.OrgID.String()
		orgIDStr = &value
	}

	return personaResponse{
		ID:                  s.ID.String(),
		OrgID:               orgIDStr,
		PersonaKey:          s.PersonaKey,
		Version:             s.Version,
		DisplayName:         s.DisplayName,
		Description:         s.Description,
		UserSelectable:      false,
		PromptMD:            s.PromptMD,
		ToolAllowlist:       allowlist,
		ToolDenylist:        denylist,
		BudgetsJSON:         budgets,
		IsActive:            s.IsActive,
		CreatedAt:           s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		PreferredCredential: optionalTrimmedStringPtr(s.PreferredCredential),
		Model:               optionalTrimmedStringPtr(s.Model),
		ReasoningMode:       reasoningMode,
		PromptCacheControl:  promptCacheControl,
		ExecutorType:        executorType,
		ExecutorConfigJSON:  executorConfig,
		Source:              "custom",
	}
}

func toBuiltinPersonaResponse(s repopersonas.RepoPersona) personaResponse {
	allowlist := s.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	denylist := s.ToolDenylist
	if denylist == nil {
		denylist = []string{}
	}
	budgets := json.RawMessage("{}")
	if len(s.Budgets) > 0 {
		if encoded, err := json.Marshal(s.Budgets); err == nil {
			budgets = encoded
		}
	}
	executorConfig := json.RawMessage("{}")
	if len(s.ExecutorConfig) > 0 {
		if encoded, err := json.Marshal(s.ExecutorConfig); err == nil {
			executorConfig = encoded
		}
	}
	executorType := strings.TrimSpace(s.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}
	reasoningMode := strings.TrimSpace(s.ReasoningMode)
	if reasoningMode == "" {
		reasoningMode = "auto"
	}
	promptCacheControl := strings.TrimSpace(s.PromptCacheControl)
	if promptCacheControl == "" {
		promptCacheControl = "none"
	}

	var description *string
	if trimmed := strings.TrimSpace(s.Description); trimmed != "" {
		description = &trimmed
	}

	return personaResponse{
		ID:                  "builtin:" + s.ID + ":" + s.Version,
		OrgID:               nil,
		PersonaKey:          s.ID,
		Version:             s.Version,
		DisplayName:         s.Title,
		Description:         description,
		UserSelectable:      s.UserSelectable,
		SelectorName:        optionalTrimmedString(s.SelectorName),
		SelectorOrder:       s.SelectorOrder,
		PromptMD:            s.PromptMD,
		ToolAllowlist:       allowlist,
		ToolDenylist:        denylist,
		BudgetsJSON:         budgets,
		IsActive:            true,
		CreatedAt:           "",
		PreferredCredential: optionalTrimmedString(s.PreferredCredential),
		Model:               optionalTrimmedString(s.Model),
		ReasoningMode:       reasoningMode,
		PromptCacheControl:  promptCacheControl,
		ExecutorType:        executorType,
		ExecutorConfigJSON:  executorConfig,
		Source:              "builtin",
	}
}

func optionalTrimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return optionalTrimmedString(*value)
}

func optionalTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
