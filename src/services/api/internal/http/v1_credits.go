package http

import (
	nethttp "net/http"
	"strconv"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type creditBalanceResponse struct {
	OrgID   string `json:"org_id"`
	Balance int64  `json:"balance"`
}

type creditTransactionResponse struct {
	ID            string  `json:"id"`
	OrgID         string  `json:"org_id"`
	Amount        int64   `json:"amount"`
	Type          string  `json:"type"`
	ReferenceType *string `json:"reference_type,omitempty"`
	ReferenceID   *string `json:"reference_id,omitempty"`
	Note          *string `json:"note,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

type meCreditsResponse struct {
	Balance      int64                       `json:"balance"`
	Transactions []creditTransactionResponse `json:"transactions"`
}

type adminAdjustRequest struct {
	OrgID  string `json:"org_id"`
	Amount int64  `json:"amount"`
	Note   string `json:"note"`
}

// meCredits 处理 GET /v1/me/credits
func meCredits(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	creditsRepo *data.CreditsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if creditsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		ctx := r.Context()

		var balance int64
		credit, err := creditsRepo.GetBalance(ctx, actor.OrgID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if credit != nil {
			balance = credit.Balance
		}

		txns, err := creditsRepo.ListTransactions(ctx, actor.OrgID, 20, 0)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusOK, meCreditsResponse{
			Balance:      balance,
			Transactions: toCreditTransactionResponses(txns),
		})
	}
}

// adminCreditsEntry 处理 GET /v1/admin/credits?org_id=
func adminCreditsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	creditsRepo *data.CreditsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if creditsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		orgIDStr := strings.TrimSpace(r.URL.Query().Get("org_id"))
		if orgIDStr == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "org_id is required", traceID, nil)
			return
		}
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org_id", traceID, nil)
			return
		}

		ctx := r.Context()

		var balance int64
		credit, err := creditsRepo.GetBalance(ctx, orgID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if credit != nil {
			balance = credit.Balance
		}

		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		txns, err := creditsRepo.ListTransactions(ctx, orgID, limit, offset)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		type adminCreditsResponse struct {
			OrgID        string                      `json:"org_id"`
			Balance      int64                       `json:"balance"`
			Transactions []creditTransactionResponse `json:"transactions"`
		}
		writeJSON(w, traceID, nethttp.StatusOK, adminCreditsResponse{
			OrgID:        orgID.String(),
			Balance:      balance,
			Transactions: toCreditTransactionResponses(txns),
		})
	}
}

// adminCreditsAdjust 处理 POST /v1/admin/credits/adjust
func adminCreditsAdjust(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	creditsRepo *data.CreditsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if creditsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		var req adminAdjustRequest
		if err := decodeJSON(r, &req); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		req.Note = strings.TrimSpace(req.Note)
		if req.Note == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "note is required", traceID, nil)
			return
		}
		if req.Amount == 0 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "amount must not be zero", traceID, nil)
			return
		}

		orgID, err := uuid.Parse(req.OrgID)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org_id", traceID, nil)
			return
		}

		ctx := r.Context()
		note := req.Note

		if req.Amount > 0 {
			err = creditsRepo.Add(ctx, orgID, req.Amount, "admin_adjustment", nil, nil, &note)
		} else {
			err = creditsRepo.Deduct(ctx, orgID, -req.Amount, "admin_adjustment", nil, nil, &note)
		}
		if err != nil {
			if _, ok := err.(data.InsufficientCreditsError); ok {
				WriteError(w, nethttp.StatusConflict, "credits.insufficient", "insufficient credits", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		credit, err := creditsRepo.GetBalance(ctx, orgID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		var balance int64
		if credit != nil {
			balance = credit.Balance
		}
		writeJSON(w, traceID, nethttp.StatusOK, creditBalanceResponse{
			OrgID:   orgID.String(),
			Balance: balance,
		})
	}
}

func toCreditTransactionResponses(txns []data.CreditTransaction) []creditTransactionResponse {
	result := make([]creditTransactionResponse, 0, len(txns))
	for _, t := range txns {
		resp := creditTransactionResponse{
			ID:        t.ID.String(),
			OrgID:     t.OrgID.String(),
			Amount:    t.Amount,
			Type:      t.Type,
			Note:      t.Note,
			CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if t.ReferenceType != nil {
			resp.ReferenceType = t.ReferenceType
		}
		if t.ReferenceID != nil {
			s := t.ReferenceID.String()
			resp.ReferenceID = &s
		}
		result = append(result, resp)
	}
	return result
}
