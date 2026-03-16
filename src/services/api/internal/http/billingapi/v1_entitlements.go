package billingapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type entitlementOverrideResponse struct {
	ID              string  `json:"id"`
	AccountID           string  `json:"account_id"`
	Key             string  `json:"key"`
	Value           string  `json:"value"`
	ValueType       string  `json:"value_type"`
	Reason          *string `json:"reason,omitempty"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type createOverrideRequest struct {
	AccountID     string  `json:"account_id"`
	Key       string  `json:"key"`
	Value     string  `json:"value"`
	ValueType string  `json:"value_type"`
	Reason    *string `json:"reason"`
	ExpiresAt *string `json:"expires_at"`
}

func entitlementOverridesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createEntitlementOverride(w, r, authService, membershipRepo, entitlementsRepo, entitlementService, apiKeysRepo, auditWriter)
		case nethttp.MethodGet:
			listEntitlementOverrides(w, r, authService, membershipRepo, entitlementsRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func entitlementOverrideEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/entitlement-overrides/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		overrideID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid override id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodDelete:
			deleteEntitlementOverride(w, r, traceID, overrideID, authService, membershipRepo, entitlementsRepo, entitlementService, apiKeysRepo, auditWriter)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createEntitlementOverride(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if entitlementsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformEntitlementsManage, w, traceID) {
		return
	}

	var req createOverrideRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	accountID, err := uuid.Parse(strings.TrimSpace(req.AccountID))
	if err != nil || accountID == uuid.Nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
		return
	}

	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "key must not be empty", traceID, nil)
		return
	}
	req.Value = strings.TrimSpace(req.Value)
	if req.Value == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "value must not be empty", traceID, nil)
		return
	}
	req.ValueType = strings.TrimSpace(req.ValueType)

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "expires_at must be RFC3339", traceID, nil)
			return
		}
		expiresAt = &t
	}

	previous, err := entitlementsRepo.GetOverrideByAccountAndKey(r.Context(), accountID, req.Key)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	override, err := entitlementsRepo.CreateOverride(
		r.Context(), accountID, req.Key, req.Value, req.ValueType,
		req.Reason, expiresAt, actor.UserID,
	)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	if entitlementService != nil {
		entitlementService.InvalidateCache(r.Context(), accountID, req.Key)
	}
	if auditWriter != nil {
		auditWriter.WriteEntitlementOverrideSet(
			r.Context(),
			traceID,
			actor.UserID,
			accountID,
			override.ID,
			override.Key,
			entitlementOverrideAuditState(previous),
			toOverrideResponse(override),
		)
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toOverrideResponse(override))
}

func listEntitlementOverrides(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if entitlementsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformEntitlementsManage, w, traceID) {
		return
	}

	accountIDStr := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountIDStr == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "account_id query parameter required", traceID, nil)
		return
	}
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
		return
	}

	overrides, err := entitlementsRepo.ListOverridesByAccount(r.Context(), accountID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]entitlementOverrideResponse, 0, len(overrides))
	for _, o := range overrides {
		resp = append(resp, toOverrideResponse(o))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func deleteEntitlementOverride(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	overrideID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if entitlementsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformEntitlementsManage, w, traceID) {
		return
	}

	accountIDStr := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountIDStr == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "account_id query parameter required", traceID, nil)
		return
	}
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
		return
	}

	previous, err := entitlementsRepo.GetOverrideByID(r.Context(), overrideID, accountID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := entitlementsRepo.DeleteOverride(r.Context(), overrideID, accountID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if entitlementService != nil && previous != nil {
		entitlementService.InvalidateCache(r.Context(), accountID, previous.Key)
	}
	if auditWriter != nil && previous != nil {
		auditWriter.WriteEntitlementOverrideDeleted(
			r.Context(),
			traceID,
			actor.UserID,
			accountID,
			previous.ID,
			previous.Key,
			entitlementOverrideAuditState(previous),
		)
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toOverrideResponse(o data.AccountEntitlementOverride) entitlementOverrideResponse {
	resp := entitlementOverrideResponse{
		ID:        o.ID.String(),
		AccountID:     o.AccountID.String(),
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

func entitlementOverrideAuditState(o *data.AccountEntitlementOverride) any {
	if o == nil {
		return nil
	}
	return toOverrideResponse(*o)
}
