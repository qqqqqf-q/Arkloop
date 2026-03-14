package billingapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type redemptionCodeResponse struct {
	ID              string  `json:"id"`
	Code            string  `json:"code"`
	Type            string  `json:"type"`
	Value           string  `json:"value"`
	MaxUses         int     `json:"max_uses"`
	UseCount        int     `json:"use_count"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
	IsActive        bool    `json:"is_active"`
	BatchID         *string `json:"batch_id,omitempty"`
	CreatedByUserID string  `json:"created_by_user_id"`
	CreatedAt       string  `json:"created_at"`
}

func toRedemptionCodeResponse(rc data.RedemptionCode) redemptionCodeResponse {
	resp := redemptionCodeResponse{
		ID:              rc.ID.String(),
		Code:            rc.Code,
		Type:            rc.Type,
		Value:           rc.Value,
		MaxUses:         rc.MaxUses,
		UseCount:        rc.UseCount,
		IsActive:        rc.IsActive,
		BatchID:         rc.BatchID,
		CreatedByUserID: rc.CreatedByUserID.String(),
		CreatedAt:       rc.CreatedAt.UTC().Format(time.RFC3339),
	}
	if rc.ExpiresAt != nil {
		s := rc.ExpiresAt.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	return resp
}

// --- POST /v1/admin/redemption-codes/batch ---

func adminRedemptionCodesBatch(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	redemptionRepo *data.RedemptionCodesRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	type batchRequest struct {
		Count     int     `json:"count"`
		Type      string  `json:"type"`
		Value     string  `json:"value"`
		MaxUses   int     `json:"max_uses"`
		ExpiresAt *string `json:"expires_at"`
		BatchID   *string `json:"batch_id"`
	}

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if redemptionRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		var req batchRequest
		if err := httpkit.DecodeJSON(r, &req); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
			return
		}

		if req.Count <= 0 || req.Count > 500 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "count must be between 1 and 500", traceID, nil)
			return
		}
		if req.Type != "credit" && req.Type != "feature" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "type must be credit or feature", traceID, nil)
			return
		}
		if strings.TrimSpace(req.Value) == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "value is required", traceID, nil)
			return
		}
		if req.Type == "credit" {
			if _, err := strconv.ParseInt(req.Value, 10, 64); err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "value must be a valid integer for credit type", traceID, nil)
				return
			}
		}
		if req.MaxUses <= 0 {
			req.MaxUses = 1
		}

		var expiresAt *time.Time
		if req.ExpiresAt != nil {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "expires_at must be RFC3339 format", traceID, nil)
				return
			}
			expiresAt = &t
		}

		ctx := r.Context()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(ctx)

		txRepo := redemptionRepo.WithTx(tx)
		codes := make([]redemptionCodeResponse, 0, req.Count)

		for i := 0; i < req.Count; i++ {
			code, genErr := data.GenerateRedemptionCode()
			if genErr != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}

			rc, createErr := txRepo.Create(ctx, code, req.Type, req.Value, req.MaxUses, expiresAt, req.BatchID, actor.UserID)
			if createErr != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			codes = append(codes, toRedemptionCodeResponse(*rc))
		}

		if err := tx.Commit(ctx); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		batchID := ""
		if req.BatchID != nil {
			batchID = *req.BatchID
		}
		if auditWriter != nil {
			auditWriter.WriteRedemptionCodeBatchCreated(ctx, traceID, actor.UserID, batchID, req.Count, req.Type)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, codes)
	}
}

// --- GET /v1/admin/redemption-codes ---

func adminRedemptionCodesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	redemptionRepo *data.RedemptionCodesRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if redemptionRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}
		beforeCreatedAt, beforeID, ok := parseThreadCursor(w, traceID, r.URL.Query())
		if !ok {
			return
		}

		query := strings.TrimSpace(r.URL.Query().Get("q"))
		codeType := strings.TrimSpace(r.URL.Query().Get("type"))

		items, err := redemptionRepo.List(r.Context(), limit, beforeCreatedAt, beforeID, query, codeType)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]redemptionCodeResponse, 0, len(items))
		for _, item := range items {
			resp = append(resp, toRedemptionCodeResponse(item))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

// --- PATCH /v1/admin/redemption-codes/{id} ---

func adminRedemptionCodeEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	redemptionRepo *data.RedemptionCodesRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		idStr := strings.TrimPrefix(r.URL.Path, "/v1/admin/redemption-codes/")
		idStr = strings.TrimRight(idStr, "/")
		id, err := uuid.Parse(idStr)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "http.not_found", "not found", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPatch:
			patchAdminRedemptionCode(w, r, id, authService, membershipRepo, redemptionRepo, apiKeysRepo, traceID)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func patchAdminRedemptionCode(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	redemptionRepo *data.RedemptionCodesRepository,
	apiKeysRepo *data.APIKeysRepository,
	traceID string,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if redemptionRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
		return
	}

	type patchBody struct {
		IsActive *bool `json:"is_active"`
	}
	var body patchBody
	if err := httpkit.DecodeJSON(r, &body); err != nil {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid request body", traceID, nil)
		return
	}
	if body.IsActive == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "is_active is required", traceID, nil)
		return
	}
	if *body.IsActive {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "only deactivation is supported", traceID, nil)
		return
	}

	rc, err := redemptionRepo.Deactivate(r.Context(), id)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if rc == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "redemption_codes.not_found", "redemption code not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toRedemptionCodeResponse(*rc))
}

// --- POST /v1/me/redeem ---

func meRedeem(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	redemptionRepo *data.RedemptionCodesRepository,
	creditsRepo *data.CreditsRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	type redeemRequest struct {
		Code string `json:"code"`
	}

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if redemptionRepo == nil || creditsRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		var req redeemRequest
		if err := httpkit.DecodeJSON(r, &req); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
			return
		}

		code := strings.TrimSpace(strings.ToUpper(req.Code))
		if code == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "code is required", traceID, nil)
			return
		}

		ctx := r.Context()

		rc, err := redemptionRepo.GetByCode(ctx, code)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if rc == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "redemption_codes.not_found", "invalid redemption code", traceID, nil)
			return
		}

		if !rc.IsActive {
			httpkit.WriteError(w, nethttp.StatusConflict, "redemption_codes.inactive", "redemption code is inactive", traceID, nil)
			return
		}
		if rc.ExpiresAt != nil && rc.ExpiresAt.Before(time.Now()) {
			httpkit.WriteError(w, nethttp.StatusConflict, "redemption_codes.expired", "redemption code has expired", traceID, nil)
			return
		}
		if rc.UseCount >= rc.MaxUses {
			httpkit.WriteError(w, nethttp.StatusConflict, "redemption_codes.exhausted", "redemption code has been fully used", traceID, nil)
			return
		}

		redeemed, err := redemptionRepo.HasRedeemed(ctx, rc.ID, actor.UserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if redeemed {
			httpkit.WriteError(w, nethttp.StatusConflict, "redemption_codes.already_redeemed", "already redeemed this code", traceID, nil)
			return
		}

		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(ctx)

		txRedemption := redemptionRepo.WithTx(tx)

		incremented, err := txRedemption.IncrementUseCount(ctx, rc.ID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if !incremented {
			httpkit.WriteError(w, nethttp.StatusConflict, "redemption_codes.exhausted", "redemption code is no longer available", traceID, nil)
			return
		}

		_, err = txRedemption.RecordRedemption(ctx, rc.ID, actor.UserID, actor.AccountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if rc.Type == "credit" {
			amount, _ := strconv.ParseInt(rc.Value, 10, 64)
			if amount > 0 {
				txCredits := creditsRepo.WithTx(tx)
				refType := "redemption_code"
				if err := txCredits.Add(ctx, actor.AccountID, amount, "redemption", &refType, &rc.ID, nil); err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
			}
		}

		if err := tx.Commit(ctx); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteRedemptionCodeRedeemed(ctx, traceID, actor.AccountID, actor.UserID, rc.ID, rc.Type, rc.Value)
		}

		type redeemResponse struct {
			Code  string `json:"code"`
			Type  string `json:"type"`
			Value string `json:"value"`
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, redeemResponse{
			Code:  rc.Code,
			Type:  rc.Type,
			Value: rc.Value,
		})
	}
}
