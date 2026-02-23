package http

import (
	"encoding/json"
	"errors"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type createLlmCredentialRequest struct {
	Name          string                            `json:"name"`
	Provider      string                            `json:"provider"`
	APIKey        string                            `json:"api_key"`
	BaseURL       *string                           `json:"base_url"`
	OpenAIAPIMode *string                           `json:"openai_api_mode"`
	Routes        []createLlmCredentialRouteRequest `json:"routes"`
}

type createLlmCredentialRouteRequest struct {
	Model           string          `json:"model"`
	IsDefault       bool            `json:"is_default"`
	Priority        int             `json:"priority"`
	WhenJSON        json.RawMessage `json:"when"`
	Multiplier      *float64        `json:"multiplier"`
	CostPer1kInput  *float64        `json:"cost_per_1k_input"`
	CostPer1kOutput *float64        `json:"cost_per_1k_output"`
}

type llmCredentialResponse struct {
	ID            string  `json:"id"`
	OrgID         string  `json:"org_id"`
	Provider      string  `json:"provider"`
	Name          string  `json:"name"`
	KeyPrefix     *string `json:"key_prefix"`
	BaseURL       *string `json:"base_url"`
	OpenAIAPIMode *string `json:"openai_api_mode"`
	CreatedAt     string  `json:"created_at"`
}

type llmCredentialWithRoutesResponse struct {
	llmCredentialResponse
	Routes []llmRouteResponse `json:"routes"`
}

type llmRouteResponse struct {
	ID              string          `json:"id"`
	CredentialID    string          `json:"credential_id"`
	Model           string          `json:"model"`
	Priority        int             `json:"priority"`
	IsDefault       bool            `json:"is_default"`
	WhenJSON        json.RawMessage `json:"when"`
	Multiplier      float64         `json:"multiplier"`
	CostPer1kInput  *float64        `json:"cost_per_1k_input,omitempty"`
	CostPer1kOutput *float64        `json:"cost_per_1k_output,omitempty"`
}

var validProviders = map[string]bool{
	"openai":    true,
	"anthropic": true,
}

var validOpenAIAPIModes = map[string]bool{
	"auto":             true,
	"responses":        true,
	"chat_completions": true,
}

func llmCredentialsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
	routeRepo *data.LlmRoutesRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		switch r.Method {
		case nethttp.MethodPost:
			createLlmCredential(w, r, traceID, authService, membershipRepo, credRepo, routeRepo, secretsRepo, pool)
		case nethttp.MethodGet:
			listLlmCredentials(w, r, traceID, authService, membershipRepo, credRepo, routeRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func llmCredentialEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/llm-credentials/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		credID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		if r.Method != nethttp.MethodDelete {
			writeMethodNotAllowed(w, r)
			return
		}

		deleteLlmCredential(w, r, traceID, credID, authService, membershipRepo, credRepo)
	}
}

func createLlmCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
	routeRepo *data.LlmRoutesRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if credRepo == nil || secretsRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	var req createLlmCredentialRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Provider = strings.TrimSpace(req.Provider)
	req.APIKey = strings.TrimSpace(req.APIKey)

	if req.Name == "" || req.Provider == "" || req.APIKey == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name, provider and api_key are required", traceID, nil)
		return
	}
	if !validProviders[req.Provider] {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid provider", traceID, nil)
		return
	}
	if req.OpenAIAPIMode != nil {
		if req.Provider != "openai" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "openai_api_mode only applies to openai provider", traceID, nil)
			return
		}
		if !validOpenAIAPIModes[*req.OpenAIAPIMode] {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid openai_api_mode", traceID, nil)
			return
		}
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	txSecrets := secretsRepo.WithTx(tx)
	txCreds := credRepo.WithTx(tx)

	credID := uuid.New()
	secret, err := txSecrets.Create(r.Context(), actor.OrgID, "llm_cred:"+credID.String(), req.APIKey)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	keyPrefix := computeKeyPrefix(req.APIKey)
	cred, err := txCreds.Create(
		r.Context(),
		credID,
		actor.OrgID,
		req.Provider,
		req.Name,
		&secret.ID,
		&keyPrefix,
		req.BaseURL,
		req.OpenAIAPIMode,
	)
	if err != nil {
		var nameConflict data.LlmCredentialNameConflictError
		if errors.As(err, &nameConflict) {
			WriteError(w, nethttp.StatusConflict, "llm_credentials.name_conflict", "credential name already exists", traceID, nil)
			return
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	routes := []llmRouteResponse{}
	if routeRepo != nil {
		txRoutes := routeRepo.WithTx(tx)
		for _, rr := range req.Routes {
			rr.Model = strings.TrimSpace(rr.Model)
			if rr.Model == "" {
				continue
			}
			whenJSON := rr.WhenJSON
			if len(whenJSON) == 0 {
				whenJSON = json.RawMessage("{}")
			}
			multiplier := 1.0
			if rr.Multiplier != nil && *rr.Multiplier > 0 {
				multiplier = *rr.Multiplier
			}
			route, err := txRoutes.Create(
				r.Context(),
				actor.OrgID,
				cred.ID,
				rr.Model,
				rr.Priority,
				rr.IsDefault,
				whenJSON,
				multiplier,
				rr.CostPer1kInput,
				rr.CostPer1kOutput,
			)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			routes = append(routes, toLlmRouteResponse(route))
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, llmCredentialWithRoutesResponse{
		llmCredentialResponse: toLlmCredentialResponse(cred),
		Routes:                routes,
	})
}

func listLlmCredentials(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
	routeRepo *data.LlmRoutesRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if credRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	creds, err := credRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 一次批量查出所有路由，按 credential_id 分组
	routesByCredID := map[uuid.UUID][]llmRouteResponse{}
	if routeRepo != nil {
		allRoutes, err := routeRepo.ListByOrg(r.Context(), actor.OrgID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		for _, route := range allRoutes {
			routesByCredID[route.CredentialID] = append(routesByCredID[route.CredentialID], toLlmRouteResponse(route))
		}
	}

	resp := make([]llmCredentialWithRoutesResponse, 0, len(creds))
	for _, cred := range creds {
		routes := routesByCredID[cred.ID]
		if routes == nil {
			routes = []llmRouteResponse{}
		}
		resp = append(resp, llmCredentialWithRoutesResponse{
			llmCredentialResponse: toLlmCredentialResponse(cred),
			Routes:                routes,
		})
	}

	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func deleteLlmCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	credID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if credRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	existing, err := credRepo.GetByID(r.Context(), actor.OrgID, credID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		WriteError(w, nethttp.StatusNotFound, "llm_credentials.not_found", "credential not found", traceID, nil)
		return
	}

	if err := credRepo.Delete(r.Context(), actor.OrgID, credID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toLlmCredentialResponse(c data.LlmCredential) llmCredentialResponse {
	return llmCredentialResponse{
		ID:            c.ID.String(),
		OrgID:         c.OrgID.String(),
		Provider:      c.Provider,
		Name:          c.Name,
		KeyPrefix:     c.KeyPrefix,
		BaseURL:       c.BaseURL,
		OpenAIAPIMode: c.OpenAIAPIMode,
		CreatedAt:     c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func toLlmRouteResponse(r data.LlmRoute) llmRouteResponse {
	whenJSON := r.WhenJSON
	if len(whenJSON) == 0 {
		whenJSON = json.RawMessage("{}")
	}
	return llmRouteResponse{
		ID:              r.ID.String(),
		CredentialID:    r.CredentialID.String(),
		Model:           r.Model,
		Priority:        r.Priority,
		IsDefault:       r.IsDefault,
		WhenJSON:        whenJSON,
		Multiplier:      r.Multiplier,
		CostPer1kInput:  r.CostPer1kInput,
		CostPer1kOutput: r.CostPer1kOutput,
	}
}

// computeKeyPrefix 取 API Key 前 8 个 UTF-8 字符用于展示识别。
func computeKeyPrefix(apiKey string) string {
	runes := []rune(apiKey)
	if len(runes) <= 8 {
		return string(runes)
	}
	return string(runes[:8])
}
