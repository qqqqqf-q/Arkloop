package catalogapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"

	"github.com/google/uuid"

	nethttp "net/http"
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
	RolesJSON              json.RawMessage `json:"roles"`
	PreferredCredential    *string         `json:"preferred_credential"`
	Model                  *string         `json:"model"`
	ReasoningMode          string          `json:"reasoning_mode"`
	PromptCacheControl     string          `json:"prompt_cache_control"`
	ExecutorType           string          `json:"executor_type"`
	ExecutorConfigJSON     json.RawMessage `json:"executor_config"`
	Scope                  string          `json:"scope"`
}

type patchPersonaRequest struct {
	DisplayName         *string         `json:"display_name"`
	Description         *string         `json:"description"`
	PromptMD            *string         `json:"prompt_md"`
	ToolAllowlist       []string        `json:"tool_allowlist"`
	ToolDenylist        []string        `json:"tool_denylist"`
	BudgetsJSON         json.RawMessage `json:"budgets"`
	RolesJSON           json.RawMessage `json:"roles"`
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
	ProjectID           *string         `json:"project_id"`
	Scope               string          `json:"scope"`
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
	RolesJSON           json.RawMessage `json:"roles"`
	IsActive            bool            `json:"is_active"`
	CreatedAt           string          `json:"created_at"`
	PreferredCredential *string         `json:"preferred_credential,omitempty"`
	Model               *string         `json:"model,omitempty"`
	ReasoningMode       string          `json:"reasoning_mode"`
	PromptCacheControl  string          `json:"prompt_cache_control"`
	ExecutorType        string          `json:"executor_type"`
	ExecutorConfigJSON  json.RawMessage `json:"executor_config"`
	SyncMode            string          `json:"sync_mode,omitempty"`
	MirroredFilePath    *string         `json:"mirrored_file_path,omitempty"`
	LastSyncedAt        *string         `json:"last_synced_at,omitempty"`
	Source              string          `json:"source"`
}

func personasEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	syncTrigger personaSyncTrigger,
	projectRepo *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodPost:
			createPersona(w, r, traceID, authService, membershipRepo, personasRepo, repoPersonas, syncTrigger, projectRepo)
		case nethttp.MethodGet:
			listPersonas(w, r, traceID, authService, membershipRepo, personasRepo, repoPersonas, projectRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func personaEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	syncTrigger personaSyncTrigger,
	projectRepo *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/personas/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		personaID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPatch:
			patchPersona(w, r, traceID, personaID, authService, membershipRepo, personasRepo, syncTrigger, projectRepo)
		case nethttp.MethodDelete:
			deletePersona(w, r, traceID, personaID, authService, membershipRepo, personasRepo, repoPersonas, syncTrigger, projectRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createPersona(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	syncTrigger personaSyncTrigger,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if personasRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	var req createPersonaRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, req.Scope, true, true)
	if !ok {
		return
	}

	projectID := uuid.Nil
	if scope == data.PersonaScopeProject {
		var resolved bool
		projectID, resolved = resolvePersonaProjectID(r.Context(), w, traceID, actor, projectRepo)
		if !resolved {
			return
		}
	}

	req.PersonaKey = strings.TrimSpace(req.PersonaKey)
	req.Version = strings.TrimSpace(req.Version)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.PromptMD = strings.TrimSpace(req.PromptMD)
	req.CopyFromRepoPersonaKey = strings.TrimSpace(req.CopyFromRepoPersonaKey)
	if err := validateRuntimeExecutorConfigRequest(req.ExecutorType, req.ExecutorConfigJSON); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	if req.CopyFromRepoPersonaKey != "" {
		repoPersona, exists := findRepoPersonaByKey(repoPersonas, req.CopyFromRepoPersonaKey)
		if !exists {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "copy_from_repo_persona_key is invalid", traceID, nil)
			return
		}
		if repoPersona.IsSystem {
			httpkit.WriteError(w, nethttp.StatusForbidden, "personas.system_protected", "system persona cannot be created via API", traceID, nil)
			return
		}
		if req.PersonaKey != "" && req.PersonaKey != repoPersona.ID {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "persona_key must match repo persona key", traceID, nil)
			return
		}
		if req.Version != "" && req.Version != repoPersona.Version {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "version must match repo persona version", traceID, nil)
			return
		}

		persona, err := materializeRepoPersonaForCreate(r.Context(), personasRepo, projectID, scope, *repoPersona, req)
		if err != nil {
			var conflict data.PersonaConflictError
			if errors.As(err, &conflict) {
				httpkit.WriteError(w, nethttp.StatusConflict, "personas.conflict", "persona with this key and version already exists", traceID, nil)
				return
			}
			if isPersonaValidationError(err) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toPersonaResponse(persona))
		notifyPersonaSync(syncTrigger)
		return
	}

	if req.PersonaKey == "" || req.Version == "" || req.DisplayName == "" || req.PromptMD == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "persona_key, version, display_name, and prompt_md are required", traceID, nil)
		return
	}

	if isSystemPersonaKey(repoPersonas, req.PersonaKey) {
		httpkit.WriteError(w, nethttp.StatusForbidden, "personas.system_protected", "system persona cannot be created via API", traceID, nil)
		return
	}

	persona, err := personasRepo.CreateInScope(
		r.Context(),
		projectID,
		scope,
		req.PersonaKey,
		req.Version,
		req.DisplayName,
		req.Description,
		req.PromptMD,
		req.ToolAllowlist,
		req.ToolDenylist,
		req.BudgetsJSON,
		req.RolesJSON,
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
			httpkit.WriteError(w, nethttp.StatusConflict, "personas.conflict", "persona with this key and version already exists", traceID, nil)
			return
		}
		if isPersonaValidationError(err) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toPersonaResponse(persona))
	notifyPersonaSync(syncTrigger)
}

func listPersonas(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if personasRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, r.URL.Query().Get("scope"), false, false)
	if !ok {
		return
	}

	projectID := uuid.Nil
	if scope == data.PersonaScopeProject {
		var resolved bool
		projectID, resolved = resolvePersonaProjectID(r.Context(), w, traceID, actor, projectRepo)
		if !resolved {
			return
		}
	}

	dbPersonas, err := personasRepo.ListByScope(r.Context(), projectID, scope)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
		resp = append(resp, toBuiltinPersonaResponse(persona, scope))
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func selectablePersonasEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	projectRepo *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if personasRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		projectID, resolved := resolvePersonaProjectID(r.Context(), w, traceID, actor, projectRepo)
		if !resolved {
			return
		}

		resp, err := buildSelectablePersonaResponses(r.Context(), projectID, personasRepo, repoPersonas)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func buildSelectablePersonaResponses(
	ctx context.Context,
	projectID uuid.UUID,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
) ([]personaResponse, error) {
	builtinByKey := make(map[string]personaResponse, len(repoPersonas))
	for _, persona := range repoPersonas {
		if !persona.UserSelectable {
			continue
		}
		builtinByKey[persona.ID] = toBuiltinPersonaResponse(persona, data.PersonaScopePlatform)
	}

	effectiveByKey := make(map[string]personaResponse, len(builtinByKey))
	for key, persona := range builtinByKey {
		effectiveByKey[key] = persona
	}

	dbPersonas, err := personasRepo.ListActiveEffective(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, persona := range dbPersonas {
		response := toPersonaResponse(persona)
		if previous, ok := effectiveByKey[persona.PersonaKey]; ok {
			response.UserSelectable = previous.UserSelectable
			response.SelectorName = previous.SelectorName
			response.SelectorOrder = previous.SelectorOrder
		}
		effectiveByKey[persona.PersonaKey] = response
	}

	resp := make([]personaResponse, 0, len(effectiveByKey))
	for _, persona := range effectiveByKey {
		if !persona.UserSelectable {
			continue
		}
		resp = append(resp, persona)
	}

	sort.Slice(resp, func(i, j int) bool {
		leftOrder := selectablePersonaOrder(resp[i])
		rightOrder := selectablePersonaOrder(resp[j])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		leftName := strings.TrimSpace(selectablePersonaLabel(resp[i]))
		rightName := strings.TrimSpace(selectablePersonaLabel(resp[j]))
		if leftName != rightName {
			return leftName < rightName
		}
		return resp[i].PersonaKey < resp[j].PersonaKey
	})

	return resp, nil
}

func selectablePersonaOrder(persona personaResponse) int {
	if persona.SelectorOrder == nil {
		return 99
	}
	return *persona.SelectorOrder
}

func selectablePersonaLabel(persona personaResponse) string {
	if persona.SelectorName != nil && strings.TrimSpace(*persona.SelectorName) != "" {
		return strings.TrimSpace(*persona.SelectorName)
	}
	return strings.TrimSpace(persona.DisplayName)
}

func patchPersona(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	syncTrigger personaSyncTrigger,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if personasRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, r.URL.Query().Get("scope"), false, true)
	if !ok {
		return
	}

	scopeID := uuid.Nil
	if scope == data.PersonaScopeProject {
		var resolved bool
		scopeID, resolved = resolvePersonaProjectID(r.Context(), w, traceID, actor, projectRepo)
		if !resolved {
			return
		}
	}

	existing, err := personasRepo.GetByIDInScope(r.Context(), scopeID, personaID, scope)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "personas.not_found", "persona not found", traceID, nil)
		return
	}

	var req patchPersonaRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	patch := data.PersonaPatch{
		DisplayName:         req.DisplayName,
		Description:         req.Description,
		PromptMD:            req.PromptMD,
		ToolAllowlist:       req.ToolAllowlist,
		ToolDenylist:        req.ToolDenylist,
		BudgetsJSON:         req.BudgetsJSON,
		RolesJSON:           req.RolesJSON,
		IsActive:            req.IsActive,
		PreferredCredential: req.PreferredCredential,
		Model:               req.Model,
		ReasoningMode:       req.ReasoningMode,
		PromptCacheControl:  req.PromptCacheControl,
		ExecutorType:        req.ExecutorType,
		ExecutorConfigJSON:  req.ExecutorConfigJSON,
	}
	if err := validateRuntimeExecutorConfigRequest(ptrStringValue(req.ExecutorType), req.ExecutorConfigJSON); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	updated, err := personasRepo.PatchInScope(r.Context(), scopeID, personaID, scope, patch)
	if err != nil {
		if isPersonaValidationError(err) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "personas.not_found", "persona not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toPersonaResponse(*updated))
	notifyPersonaSync(syncTrigger)
}

func deletePersona(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	personasRepo *data.PersonasRepository,
	repoPersonas []repopersonas.RepoPersona,
	syncTrigger personaSyncTrigger,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if personasRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope, ok := requirePersonaScope(actor, w, traceID, r.URL.Query().Get("scope"), false, true)
	if !ok {
		return
	}

	scopeID := uuid.Nil
	if scope == data.PersonaScopeProject {
		var resolved bool
		scopeID, resolved = resolvePersonaProjectID(r.Context(), w, traceID, actor, projectRepo)
		if !resolved {
			return
		}
	}

	existing, err := personasRepo.GetByIDInScope(r.Context(), scopeID, personaID, scope)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "personas.not_found", "persona not found", traceID, nil)
		return
	}
	if isSystemPersonaKey(repoPersonas, existing.PersonaKey) {
		httpkit.WriteError(w, nethttp.StatusForbidden, "personas.system_protected", "cannot delete system persona", traceID, nil)
		return
	}

	deleted, err := personasRepo.DeleteInScope(r.Context(), scopeID, personaID, scope)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !deleted {
		httpkit.WriteError(w, nethttp.StatusNotFound, "personas.not_found", "persona not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"ok": true})
	notifyPersonaSync(syncTrigger)
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
	roles := s.RolesJSON
	if len(roles) == 0 {
		roles = json.RawMessage("{}")
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

	var projectIDStr *string
	if s.ProjectID != nil {
		value := s.ProjectID.String()
		projectIDStr = &value
	}

	return personaResponse{
		ID:                  s.ID.String(),
		ProjectID:           projectIDStr,
		Scope:               personaScopeFromProjectID(s.ProjectID),
		PersonaKey:          s.PersonaKey,
		Version:             s.Version,
		DisplayName:         s.DisplayName,
		Description:         s.Description,
		UserSelectable:      s.UserSelectable,
		SelectorName:        optionalTrimmedStringPtr(s.SelectorName),
		SelectorOrder:       s.SelectorOrder,
		PromptMD:            s.PromptMD,
		ToolAllowlist:       allowlist,
		ToolDenylist:        denylist,
		BudgetsJSON:         budgets,
		RolesJSON:           roles,
		IsActive:            s.IsActive,
		CreatedAt:           s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		PreferredCredential: optionalTrimmedStringPtr(s.PreferredCredential),
		Model:               optionalTrimmedStringPtr(s.Model),
		ReasoningMode:       reasoningMode,
		PromptCacheControl:  promptCacheControl,
		ExecutorType:        executorType,
		ExecutorConfigJSON:  executorConfig,
		SyncMode:            strings.TrimSpace(s.SyncMode),
		MirroredFilePath:    mirroredPersonaFilePath(s.SyncMode, s.MirroredFileDir),
		LastSyncedAt:        optionalTimeString(s.LastSyncedAt),
		Source:              "custom",
	}
}

func toBuiltinPersonaResponse(s repopersonas.RepoPersona, scope string) personaResponse {
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
	roles := json.RawMessage("{}")
	if len(s.Roles) > 0 {
		if encoded, err := json.Marshal(s.Roles); err == nil {
			roles = encoded
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
		Scope:               scope,
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
		RolesJSON:           roles,
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

func requirePersonaScope(actor *httpkit.Actor, w nethttp.ResponseWriter, traceID, rawScope string, fromBody bool, write bool) (string, bool) {
	scope := strings.TrimSpace(rawScope)
	if scope == "" {
		scope = data.PersonaScopePlatform
	}
	normalized, err := data.NormalizePersonaScope(scope)
	if err != nil {
		message := "scope must be project or platform"
		if fromBody {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", message, traceID, nil)
		} else {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", message, traceID, nil)
		}
		return "", false
	}
	if normalized == data.PersonaScopePlatform {
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return "", false
		}
		return normalized, true
	}
	requiredPerm := auth.PermDataPersonasRead
	if write {
		requiredPerm = auth.PermDataPersonasManage
	}
	if !httpkit.RequirePerm(actor, requiredPerm, w, traceID) {
		return "", false
	}
	return normalized, true
}

func mirroredPersonaFilePath(syncMode string, mirroredFileDir *string) *string {
	if strings.TrimSpace(syncMode) != data.PersonaSyncModePlatformFileMirror || mirroredFileDir == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*mirroredFileDir)
	if trimmed == "" {
		return nil
	}
	value := filepath.ToSlash(filepath.Join(trimmed, "persona.yaml"))
	return &value
}

func optionalTimeString(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format("2006-01-02T15:04:05Z")
	return &formatted
}

func notifyPersonaSync(syncTrigger personaSyncTrigger) {
	if syncTrigger != nil {
		syncTrigger.Trigger()
	}
}

func validateRuntimeExecutorConfigRequest(executorType string, raw json.RawMessage) error {
	if strings.TrimSpace(executorType) != "agent.lua" {
		return nil
	}
	if len(raw) == 0 {
		return fmt.Errorf("executor_config.script is required for agent.lua runtime")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("executor_config must be valid json object")
	}
	if _, exists := obj["script_file"]; exists {
		return fmt.Errorf("executor_config.script_file is not allowed for agent.lua runtime")
	}
	script, _ := obj["script"].(string)
	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("executor_config.script is required for agent.lua runtime")
	}
	return nil
}

func ptrStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func isPersonaValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "executor_config.") || strings.Contains(message, "must not be empty") || strings.Contains(message, "is required") || strings.Contains(message, "valid json object")
}

func resolvePersonaProjectID(
	ctx context.Context,
	w nethttp.ResponseWriter,
	traceID string,
	actor *httpkit.Actor,
	projectRepo *data.ProjectRepository,
) (uuid.UUID, bool) {
	if projectRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return uuid.Nil, false
	}
	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, actor.AccountID, actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return uuid.Nil, false
	}
	return project.ID, true
}

func personaScopeFromProjectID(projectID *uuid.UUID) string {
	if projectID == nil {
		return data.PersonaScopePlatform
	}
	return data.PersonaScopeProject
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

func isSystemPersonaKey(repoPersonas []repopersonas.RepoPersona, key string) bool {
	for _, rp := range repoPersonas {
		if rp.ID == key && rp.IsSystem {
			return true
		}
	}
	return false
}
