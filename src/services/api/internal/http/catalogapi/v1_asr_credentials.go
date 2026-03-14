package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"errors"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type createAsrCredentialRequest struct {
	Name      string  `json:"name"`
	Provider  string  `json:"provider"`
	APIKey    string  `json:"api_key"`
	BaseURL   *string `json:"base_url"`
	Model     string  `json:"model"`
	IsDefault bool    `json:"is_default"`
	Scope     string  `json:"scope"` // "project" | "platform"; platform requires platform_admin
}

type asrCredentialResponse struct {
	ID        string  `json:"id"`
	AccountID     *string `json:"account_id"` // null for platform scope
	Scope     string  `json:"scope"`
	Provider  string  `json:"provider"`
	Name      string  `json:"name"`
	KeyPrefix *string `json:"key_prefix"`
	BaseURL   *string `json:"base_url"`
	Model     string  `json:"model"`
	IsDefault bool    `json:"is_default"`
	CreatedAt string  `json:"created_at"`
}

var validAsrProviders = map[string]bool{
	"groq":   true,
	"openai": true,
}

func asrCredentialsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.TxStarter,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodPost:
			createAsrCredential(w, r, traceID, authService, membershipRepo, credRepo, secretsRepo, pool)
		case nethttp.MethodGet:
			listAsrCredentials(w, r, traceID, authService, membershipRepo, credRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func asrCredentialEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/asr-credentials/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		// POST /v1/asr-credentials/{id}/set-default
		if strings.HasSuffix(tail, "/set-default") {
			idStr := strings.TrimSuffix(tail, "/set-default")
			credID, err := uuid.Parse(idStr)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid credential id", traceID, nil)
				return
			}
			if r.Method != nethttp.MethodPost {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			setDefaultAsrCredential(w, r, traceID, credID, authService, membershipRepo, credRepo)
			return
		}

		credID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid credential id", traceID, nil)
			return
		}
		if r.Method != nethttp.MethodDelete {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		deleteAsrCredential(w, r, traceID, credID, authService, membershipRepo, credRepo)
	}
}

func createAsrCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.TxStarter,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if credRepo == nil || secretsRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	var req createAsrCredentialRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Provider = strings.TrimSpace(req.Provider)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Model = strings.TrimSpace(req.Model)
	normalizedBaseURL, err := normalizeOptionalBaseURL(req.BaseURL)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "base_url is invalid", traceID, nil)
		return
	}
	if req.Scope == "" {
		req.Scope = "project"
	}

	if req.Name == "" || req.Provider == "" || req.APIKey == "" || req.Model == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name, provider, api_key and model are required", traceID, nil)
		return
	}
	if !validAsrProviders[req.Provider] {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid provider", traceID, nil)
		return
	}
	if req.Scope != "project" && req.Scope != "platform" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be project or platform", traceID, nil)
		return
	}
	if req.Scope == "platform" && !actor.HasPermission(auth.PermPlatformAdmin) {
		httpkit.WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "platform scope requires platform_admin", traceID, nil)
		return
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	txSecrets := secretsRepo.WithTx(tx)
	txCreds := credRepo.WithTx(tx)

	credID := uuid.New()

	ownerKind := "user"
	var ownerUserID *uuid.UUID
	if req.Scope == "platform" {
		ownerKind = "platform"
	} else {
		ownerUserID = &actor.UserID
	}

	secret, err := txSecrets.Create(r.Context(), actor.UserID, "asr_cred:"+credID.String(), req.APIKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	keyPrefix := computeKeyPrefix(req.APIKey)

	cred, err := txCreds.Create(
		r.Context(),
		credID,
		ownerKind,
		ownerUserID,
		req.Provider,
		req.Name,
		&secret.ID,
		&keyPrefix,
		normalizedBaseURL,
		req.Model,
		req.IsDefault,
	)
	if err != nil {
		var nameConflict data.AsrCredentialNameConflictError
		if errors.As(err, &nameConflict) {
			httpkit.WriteError(w, nethttp.StatusConflict, "asr_credentials.name_conflict", "credential name already exists", traceID, nil)
			return
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toAsrCredentialResponse(cred))
}

func listAsrCredentials(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if credRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	creds, err := credRepo.ListByOwner(r.Context(), actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]asrCredentialResponse, 0, len(creds))
	for _, c := range creds {
		resp = append(resp, toAsrCredentialResponse(c))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func deleteAsrCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	credID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if credRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	isPlatformAdmin := actor.HasPermission(auth.PermPlatformAdmin)

	existing, err := credRepo.GetByID(r.Context(), "platform", nil, credID)
	if err == nil && existing == nil {
		existing, err = credRepo.GetByID(r.Context(), "user", &actor.UserID, credID)
	}
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil || (existing.OwnerKind == "platform" && !isPlatformAdmin) {
		httpkit.WriteError(w, nethttp.StatusNotFound, "asr_credentials.not_found", "credential not found", traceID, nil)
		return
	}

	if err := credRepo.Delete(r.Context(), existing.OwnerKind, existing.OwnerUserID, credID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func setDefaultAsrCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	credID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if credRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	isPlatformAdmin := actor.HasPermission(auth.PermPlatformAdmin)

	existing, err := credRepo.GetByID(r.Context(), "platform", nil, credID)
	if err == nil && existing == nil {
		existing, err = credRepo.GetByID(r.Context(), "user", &actor.UserID, credID)
	}
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil || (existing.OwnerKind == "platform" && !isPlatformAdmin) {
		httpkit.WriteError(w, nethttp.StatusNotFound, "asr_credentials.not_found", "credential not found", traceID, nil)
		return
	}

	if existing.OwnerKind == "platform" {
		if err := credRepo.SetDefaultPlatform(r.Context(), credID); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	} else {
		if err := credRepo.SetDefault(r.Context(), actor.UserID, credID); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toAsrCredentialResponse(c data.AsrCredential) asrCredentialResponse {
	var accountID *string
	if c.OwnerUserID != nil {
		s := c.OwnerUserID.String()
		accountID = &s
	}
	scope := c.OwnerKind
	if scope == "user" {
		scope = "project"
	}
	return asrCredentialResponse{
		ID:        c.ID.String(),
		AccountID:     accountID,
		Scope:     scope,
		Provider:  c.Provider,
		Name:      c.Name,
		KeyPrefix: c.KeyPrefix,
		BaseURL:   c.BaseURL,
		Model:     c.Model,
		IsDefault: c.IsDefault,
		CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
