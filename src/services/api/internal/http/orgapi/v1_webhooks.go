package orgapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var validWebhookEvents = map[string]struct{}{
	"run.completed": {},
	"run.failed":    {},
	"run.cancelled": {},
}

type webhookEndpointResponse struct {
	ID        string   `json:"id"`
	AccountID     string   `json:"account_id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
}

type createWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

type updateWebhookRequest struct {
	Enabled *bool `json:"enabled"`
}

func webhookEndpointsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createWebhookEndpoint(w, r, authService, membershipRepo, webhookRepo, apiKeysRepo, secretsRepo, pool)
		case nethttp.MethodGet:
			listWebhookEndpoints(w, r, authService, membershipRepo, webhookRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func webhookEndpointEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/webhook-endpoints/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		endpointID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid endpoint id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getWebhookEndpoint(w, r, traceID, endpointID, authService, membershipRepo, webhookRepo, apiKeysRepo)
		case nethttp.MethodPatch:
			updateWebhookEndpoint(w, r, traceID, endpointID, authService, membershipRepo, webhookRepo, apiKeysRepo)
		case nethttp.MethodDelete:
			deleteWebhookEndpoint(w, r, traceID, endpointID, authService, membershipRepo, webhookRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}
	if secretsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	var req createWebhookRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "url must not be empty", traceID, nil)
		return
	}
	if !strings.HasPrefix(req.URL, "https://") && !strings.HasPrefix(req.URL, "http://") {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "url must start with http:// or https://", traceID, nil)
		return
	}

	if len(req.Events) == 0 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "events must not be empty", traceID, nil)
		return
	}
	for _, ev := range req.Events {
		if _, ok := validWebhookEvents[ev]; !ok {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "unsupported event type: "+ev, traceID, nil)
			return
		}
	}

	signingSecret, err := generateSigningSecret()
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	endpointID := uuid.New()

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	secret, err := secretsRepo.WithTx(tx).Create(r.Context(), actor.AccountID, data.WebhookSecretName(endpointID), signingSecret)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	ep, err := webhookRepo.WithTx(tx).Create(r.Context(), endpointID, actor.AccountID, req.URL, secret.ID, req.Events)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toWebhookEndpointResponse(ep))
}

func listWebhookEndpoints(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	endpoints, err := webhookRepo.ListByOrg(r.Context(), actor.AccountID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]webhookEndpointResponse, 0, len(endpoints))
	for _, ep := range endpoints {
		resp = append(resp, toWebhookEndpointResponse(ep))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func getWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	endpointID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	ep, err := webhookRepo.GetByID(r.Context(), endpointID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ep == nil || ep.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toWebhookEndpointResponse(*ep))
}

func updateWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	endpointID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	// 先查找端点，验证归属
	ep, err := webhookRepo.GetByID(r.Context(), endpointID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ep == nil || ep.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	var req updateWebhookRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	if req.Enabled == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "enabled field required", traceID, nil)
		return
	}

	updated, err := webhookRepo.SetEnabled(r.Context(), endpointID, *req.Enabled)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toWebhookEndpointResponse(*updated))
}

func deleteWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	endpointID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	ep, err := webhookRepo.GetByID(r.Context(), endpointID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ep == nil || ep.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	if err := webhookRepo.Delete(r.Context(), endpointID, actor.AccountID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toWebhookEndpointResponse(ep data.WebhookEndpoint) webhookEndpointResponse {
	events := ep.Events
	if events == nil {
		events = []string{}
	}
	return webhookEndpointResponse{
		ID:        ep.ID.String(),
		AccountID:     ep.AccountID.String(),
		URL:       ep.URL,
		Events:    events,
		Enabled:   ep.Enabled,
		CreatedAt: ep.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func generateSigningSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
