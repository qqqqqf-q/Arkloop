package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type entitlementOverrideResponse struct {
	ID              string  `json:"id"`
	OrgID           string  `json:"org_id"`
	Key             string  `json:"key"`
	Value           string  `json:"value"`
	ValueType       string  `json:"value_type"`
	Reason          *string `json:"reason,omitempty"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type createOverrideRequest struct {
	OrgID     string  `json:"org_id"`
	Key       string  `json:"key"`
	Value     string  `json:"value"`
	ValueType string  `json:"value_type"`
	Reason    *string `json:"reason"`
	ExpiresAt *string `json:"expires_at"`
}

func entitlementOverridesEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createEntitlementOverride(w, r, authService, membershipRepo, entitlementsRepo, entitlementService, apiKeysRepo)
		case nethttp.MethodGet:
			listEntitlementOverrides(w, r, authService, membershipRepo, entitlementsRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func entitlementOverrideEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/entitlement-overrides/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		overrideID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid override id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodDelete:
			deleteEntitlementOverride(w, r, traceID, overrideID, authService, membershipRepo, entitlementsRepo, entitlementService, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func createEntitlementOverride(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if entitlementsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermPlatformEntitlementsManage, w, traceID) {
		return
	}

	var req createOverrideRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	orgID, err := uuid.Parse(strings.TrimSpace(req.OrgID))
	if err != nil || orgID == uuid.Nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org_id", traceID, nil)
		return
	}

	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "key must not be empty", traceID, nil)
		return
	}
	req.Value = strings.TrimSpace(req.Value)
	if req.Value == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "value must not be empty", traceID, nil)
		return
	}
	req.ValueType = strings.TrimSpace(req.ValueType)

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "expires_at must be RFC3339", traceID, nil)
			return
		}
		expiresAt = &t
	}

	override, err := entitlementsRepo.CreateOverride(
		r.Context(), orgID, req.Key, req.Value, req.ValueType,
		req.Reason, expiresAt, actor.UserID,
	)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	// 缓存失效
	if entitlementService != nil {
		entitlementService.InvalidateCache(r.Context(), orgID, req.Key)
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toOverrideResponse(override))
}

func listEntitlementOverrides(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if entitlementsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermPlatformEntitlementsManage, w, traceID) {
		return
	}

	orgIDStr := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if orgIDStr == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "org_id query parameter required", traceID, nil)
		return
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org_id", traceID, nil)
		return
	}

	overrides, err := entitlementsRepo.ListOverridesByOrg(r.Context(), orgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]entitlementOverrideResponse, 0, len(overrides))
	for _, o := range overrides {
		resp = append(resp, toOverrideResponse(o))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func deleteEntitlementOverride(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	overrideID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if entitlementsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermPlatformEntitlementsManage, w, traceID) {
		return
	}

	orgIDStr := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if orgIDStr == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "org_id query parameter required", traceID, nil)
		return
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org_id", traceID, nil)
		return
	}

	if err := entitlementsRepo.DeleteOverride(r.Context(), overrideID, orgID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 无法精确知道 key，对整个 org 不做缓存失效（或可扩展为先查再删）
	// 这里选择简单方案：让缓存自然过期（TTL 5min）
	_ = entitlementService

	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toOverrideResponse(o data.OrgEntitlementOverride) entitlementOverrideResponse {
	resp := entitlementOverrideResponse{
		ID:        o.ID.String(),
		OrgID:     o.OrgID.String(),
		Key:       o.Key,
		Value:     o.Value,
		ValueType: o.ValueType,
		Reason:    o.Reason,
		CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if o.ExpiresAt != nil {
		t := o.ExpiresAt.UTC().Format(time.RFC3339Nano)
		resp.ExpiresAt = &t
	}
	if o.CreatedByUserID != nil {
		uid := o.CreatedByUserID.String()
		resp.CreatedByUserID = &uid
	}
	return resp
}
