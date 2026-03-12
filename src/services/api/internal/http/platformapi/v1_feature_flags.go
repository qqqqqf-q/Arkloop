package platformapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type featureFlagResponse struct {
	ID           string  `json:"id"`
	Key          string  `json:"key"`
	Description  *string `json:"description,omitempty"`
	DefaultValue bool    `json:"default_value"`
	CreatedAt    string  `json:"created_at"`
}

type accountFeatureOverrideResponse struct {
	AccountID     string `json:"account_id"`
	FlagKey   string `json:"flag_key"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

type createFeatureFlagRequest struct {
	Key          string  `json:"key"`
	Description  *string `json:"description"`
	DefaultValue bool    `json:"default_value"`
}

type setAccountOverrideRequest struct {
	AccountID   string `json:"account_id"`
	Enabled bool   `json:"enabled"`
}

type updateFeatureFlagRequest struct {
	DefaultValue *bool `json:"default_value"`
}

func featureFlagsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createFeatureFlag(w, r, authService, membershipRepo, flagRepo, apiKeysRepo, auditWriter)
		case nethttp.MethodGet:
			listFeatureFlags(w, r, authService, membershipRepo, flagRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func featureFlagEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	flagService *featureflag.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/feature-flags/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		parts := strings.SplitN(tail, "/", 3)
		flagKey := parts[0]

		if len(parts) == 1 {
			switch r.Method {
			case nethttp.MethodGet:
				getFeatureFlag(w, r, traceID, flagKey, authService, membershipRepo, flagRepo, apiKeysRepo)
			case nethttp.MethodPatch:
				updateFeatureFlag(w, r, traceID, flagKey, authService, membershipRepo, flagRepo, flagService, apiKeysRepo, auditWriter)
			case nethttp.MethodDelete:
				deleteFeatureFlag(w, r, traceID, flagKey, authService, membershipRepo, flagRepo, flagService, apiKeysRepo, auditWriter)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
			return
		}

		if parts[1] != "account-overrides" {
			httpkit.WriteNotFound(w, r)
			return
		}

		if len(parts) == 2 {
			switch r.Method {
			case nethttp.MethodPost:
				setFlagAccountOverride(w, r, traceID, flagKey, authService, membershipRepo, flagRepo, flagService, apiKeysRepo, auditWriter)
			case nethttp.MethodGet:
				listFlagAccountOverrides(w, r, traceID, flagKey, authService, membershipRepo, flagRepo, apiKeysRepo)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
			return
		}

		accountID, err := uuid.Parse(parts[2])
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodDelete:
			deleteFlagAccountOverride(w, r, traceID, flagKey, accountID, authService, membershipRepo, flagRepo, flagService, apiKeysRepo, auditWriter)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createFeatureFlag(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	var req createFeatureFlagRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "key must not be empty", traceID, nil)
		return
	}

	flag, err := flagRepo.CreateFlag(r.Context(), req.Key, req.Description, req.DefaultValue)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusConflict, "feature_flags.conflict", err.Error(), traceID, nil)
		return
	}

	state := toFeatureFlagResponse(flag)
	if auditWriter != nil {
		auditWriter.WriteFeatureFlagCreated(r.Context(), traceID, actor.UserID, flag.ID, flag.Key, state)
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, state)
}

func listFeatureFlags(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	flags, err := flagRepo.ListFlags(r.Context())
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]featureFlagResponse, 0, len(flags))
	for _, f := range flags {
		resp = append(resp, toFeatureFlagResponse(f))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func getFeatureFlag(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	flagKey string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	flag, err := flagRepo.GetFlag(r.Context(), flagKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if flag == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "feature_flags.not_found", "feature flag not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toFeatureFlagResponse(*flag))
}

func updateFeatureFlag(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	flagKey string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	flagService *featureflag.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	var req updateFeatureFlagRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	if req.DefaultValue == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "default_value is required", traceID, nil)
		return
	}

	previous, err := flagRepo.GetFlag(r.Context(), flagKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if previous == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "feature_flags.not_found", "feature flag not found", traceID, nil)
		return
	}

	flag, err := flagRepo.UpdateFlagDefaultValue(r.Context(), flagKey, *req.DefaultValue)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if flag == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "feature_flags.not_found", "feature flag not found", traceID, nil)
		return
	}

	if flagService != nil {
		flagService.InvalidateGlobalCache(r.Context(), flagKey)
	}
	if auditWriter != nil {
		auditWriter.WriteFeatureFlagUpdated(
			r.Context(),
			traceID,
			actor.UserID,
			flag.ID,
			flag.Key,
			toFeatureFlagResponse(*previous),
			toFeatureFlagResponse(*flag),
		)
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toFeatureFlagResponse(*flag))
}

func deleteFeatureFlag(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	flagKey string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	flagService *featureflag.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	previous, err := flagRepo.GetFlag(r.Context(), flagKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := flagRepo.DeleteFlag(r.Context(), flagKey); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	_ = flagService
	if auditWriter != nil && previous != nil {
		auditWriter.WriteFeatureFlagDeleted(r.Context(), traceID, actor.UserID, previous.ID, previous.Key, toFeatureFlagResponse(*previous))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func setFlagAccountOverride(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	flagKey string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	flagService *featureflag.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	var req setAccountOverrideRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	accountID, err := uuid.Parse(strings.TrimSpace(req.AccountID))
	if err != nil || accountID == uuid.Nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
		return
	}

	flag, err := flagRepo.GetFlag(r.Context(), flagKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if flag == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "feature_flags.not_found", "feature flag not found", traceID, nil)
		return
	}

	previous, err := flagRepo.GetOrgOverride(r.Context(), accountID, flagKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	override, err := flagRepo.SetOrgOverride(r.Context(), accountID, flagKey, req.Enabled)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	if flagService != nil {
		flagService.InvalidateCache(r.Context(), accountID, flagKey)
	}
	if auditWriter != nil {
		auditWriter.WriteFeatureFlagAccountOverrideSet(
			r.Context(),
			traceID,
			actor.UserID,
			accountID,
			flagKey,
			accountFeatureOverrideAuditState(previous),
			toAccountOverrideResponse(override),
		)
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toAccountOverrideResponse(override))
}

func listFlagAccountOverrides(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	flagKey string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	overrides, err := flagRepo.ListOverridesByFlag(r.Context(), flagKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]accountFeatureOverrideResponse, 0, len(overrides))
	for _, o := range overrides {
		resp = append(resp, toAccountOverrideResponse(o))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func deleteFlagAccountOverride(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	flagKey string,
	accountID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	flagRepo *data.FeatureFlagRepository,
	flagService *featureflag.Service,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if flagRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformFeatureFlagsManage, w, traceID) {
		return
	}

	previous, err := flagRepo.GetOrgOverride(r.Context(), accountID, flagKey)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := flagRepo.DeleteOrgOverride(r.Context(), accountID, flagKey); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if flagService != nil && previous != nil {
		flagService.InvalidateCache(r.Context(), accountID, flagKey)
	}
	if auditWriter != nil && previous != nil {
		auditWriter.WriteFeatureFlagAccountOverrideDeleted(
			r.Context(),
			traceID,
			actor.UserID,
			accountID,
			flagKey,
			accountFeatureOverrideAuditState(previous),
		)
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toFeatureFlagResponse(f data.FeatureFlag) featureFlagResponse {
	return featureFlagResponse{
		ID:           f.ID.String(),
		Key:          f.Key,
		Description:  f.Description,
		DefaultValue: f.DefaultValue,
		CreatedAt:    f.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toAccountOverrideResponse(o data.AccountFeatureOverride) accountFeatureOverrideResponse {
	return accountFeatureOverrideResponse{
		AccountID:     o.AccountID.String(),
		FlagKey:   o.FlagKey,
		Enabled:   o.Enabled,
		CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func accountFeatureOverrideAuditState(o *data.AccountFeatureOverride) any {
	if o == nil {
		return nil
	}
	return toAccountOverrideResponse(*o)
}
