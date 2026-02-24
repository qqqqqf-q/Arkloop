package http

import (
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

type createAsrCredentialRequest struct {
	Name      string  `json:"name"`
	Provider  string  `json:"provider"`
	APIKey    string  `json:"api_key"`
	BaseURL   *string `json:"base_url"`
	Model     string  `json:"model"`
	IsDefault bool    `json:"is_default"`
}

type asrCredentialResponse struct {
	ID        string  `json:"id"`
	OrgID     string  `json:"org_id"`
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
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodPost:
			createAsrCredential(w, r, traceID, authService, membershipRepo, credRepo, secretsRepo, pool)
		case nethttp.MethodGet:
			listAsrCredentials(w, r, traceID, authService, membershipRepo, credRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func asrCredentialEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/asr-credentials/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		// POST /v1/asr-credentials/{id}/set-default
		if strings.HasSuffix(tail, "/set-default") {
			idStr := strings.TrimSuffix(tail, "/set-default")
			credID, err := uuid.Parse(idStr)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid credential id", traceID, nil)
				return
			}
			if r.Method != nethttp.MethodPost {
				writeMethodNotAllowed(w, r)
				return
			}
			setDefaultAsrCredential(w, r, traceID, credID, authService, membershipRepo, credRepo)
			return
		}

		credID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid credential id", traceID, nil)
			return
		}
		if r.Method != nethttp.MethodDelete {
			writeMethodNotAllowed(w, r)
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
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
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

	var req createAsrCredentialRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Provider = strings.TrimSpace(req.Provider)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Model = strings.TrimSpace(req.Model)

	if req.Name == "" || req.Provider == "" || req.APIKey == "" || req.Model == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name, provider, api_key and model are required", traceID, nil)
		return
	}
	if !validAsrProviders[req.Provider] {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid provider", traceID, nil)
		return
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
	secret, err := txSecrets.Create(r.Context(), actor.OrgID, "asr_cred:"+credID.String(), req.APIKey)
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
		req.Model,
		req.IsDefault,
	)
	if err != nil {
		var nameConflict data.AsrCredentialNameConflictError
		if errors.As(err, &nameConflict) {
			WriteError(w, nethttp.StatusConflict, "asr_credentials.name_conflict", "credential name already exists", traceID, nil)
			return
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toAsrCredentialResponse(cred))
}

func listAsrCredentials(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
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

	resp := make([]asrCredentialResponse, 0, len(creds))
	for _, c := range creds {
		resp = append(resp, toAsrCredentialResponse(c))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func deleteAsrCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	credID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
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
		WriteError(w, nethttp.StatusNotFound, "asr_credentials.not_found", "credential not found", traceID, nil)
		return
	}

	if err := credRepo.Delete(r.Context(), actor.OrgID, credID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func setDefaultAsrCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	credID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.AsrCredentialsRepository,
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
		WriteError(w, nethttp.StatusNotFound, "asr_credentials.not_found", "credential not found", traceID, nil)
		return
	}

	if err := credRepo.SetDefault(r.Context(), actor.OrgID, credID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toAsrCredentialResponse(c data.AsrCredential) asrCredentialResponse {
	return asrCredentialResponse{
		ID:        c.ID.String(),
		OrgID:     c.OrgID.String(),
		Provider:  c.Provider,
		Name:      c.Name,
		KeyPrefix: c.KeyPrefix,
		BaseURL:   c.BaseURL,
		Model:     c.Model,
		IsDefault: c.IsDefault,
		CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
