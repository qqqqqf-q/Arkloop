package http

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

var validWebhookEvents = map[string]struct{}{
	"run.completed": {},
	"run.failed":    {},
	"run.cancelled": {},
}

type webhookEndpointResponse struct {
	ID        string   `json:"id"`
	OrgID     string   `json:"org_id"`
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
	membershipRepo *data.OrgMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createWebhookEndpoint(w, r, authService, membershipRepo, webhookRepo, apiKeysRepo)
		case nethttp.MethodGet:
			listWebhookEndpoints(w, r, authService, membershipRepo, webhookRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func webhookEndpointEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/webhook-endpoints/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		endpointID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid endpoint id", traceID, nil)
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
			writeMethodNotAllowed(w, r)
		}
	}
}

func createWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	var req createWebhookRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "url must not be empty", traceID, nil)
		return
	}
	if !strings.HasPrefix(req.URL, "https://") && !strings.HasPrefix(req.URL, "http://") {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "url must start with http:// or https://", traceID, nil)
		return
	}

	if len(req.Events) == 0 {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "events must not be empty", traceID, nil)
		return
	}
	for _, ev := range req.Events {
		if _, ok := validWebhookEvents[ev]; !ok {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "unsupported event type: "+ev, traceID, nil)
			return
		}
	}

	signingSecret, err := generateSigningSecret()
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	ep, err := webhookRepo.Create(r.Context(), actor.OrgID, req.URL, signingSecret, req.Events)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toWebhookEndpointResponse(ep))
}

func listWebhookEndpoints(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	endpoints, err := webhookRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]webhookEndpointResponse, 0, len(endpoints))
	for _, ep := range endpoints {
		resp = append(resp, toWebhookEndpointResponse(ep))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func getWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	endpointID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	ep, err := webhookRepo.GetByID(r.Context(), endpointID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ep == nil || ep.OrgID != actor.OrgID {
		WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toWebhookEndpointResponse(*ep))
}

func updateWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	endpointID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	// 先查找端点，验证归属
	ep, err := webhookRepo.GetByID(r.Context(), endpointID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ep == nil || ep.OrgID != actor.OrgID {
		WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	var req updateWebhookRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	if req.Enabled == nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "enabled field required", traceID, nil)
		return
	}

	updated, err := webhookRepo.SetEnabled(r.Context(), endpointID, *req.Enabled)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toWebhookEndpointResponse(*updated))
}

func deleteWebhookEndpoint(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	endpointID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	webhookRepo *data.WebhookEndpointRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if webhookRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataWebhooksManage, w, traceID) {
		return
	}

	ep, err := webhookRepo.GetByID(r.Context(), endpointID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ep == nil || ep.OrgID != actor.OrgID {
		WriteError(w, nethttp.StatusNotFound, "webhooks.not_found", "webhook endpoint not found", traceID, nil)
		return
	}

	if err := webhookRepo.Delete(r.Context(), endpointID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toWebhookEndpointResponse(ep data.WebhookEndpoint) webhookEndpointResponse {
	events := ep.Events
	if events == nil {
		events = []string{}
	}
	return webhookEndpointResponse{
		ID:        ep.ID.String(),
		OrgID:     ep.OrgID.String(),
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
