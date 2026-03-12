package billingapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"encoding/json"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type creditBalanceResponse struct {
	AccountID   string `json:"account_id"`
	Balance int64  `json:"balance"`
}

type creditTransactionResponse struct {
	ID            string           `json:"id"`
	AccountID         string           `json:"account_id"`
	Amount        int64            `json:"amount"`
	Type          string           `json:"type"`
	ReferenceType *string          `json:"reference_type,omitempty"`
	ReferenceID   *string          `json:"reference_id,omitempty"`
	Note          *string          `json:"note,omitempty"`
	Metadata      *json.RawMessage `json:"metadata,omitempty"`
	ThreadTitle   *string          `json:"thread_title,omitempty"`
	CreatedAt     string           `json:"created_at"`
}

type meCreditsResponse struct {
	Balance      int64                       `json:"balance"`
	Transactions []creditTransactionResponse `json:"transactions"`
}

type adminAdjustRequest struct {
	AccountID  string `json:"account_id"`
	Amount int64  `json:"amount"`
	Note   string `json:"note"`
}

func meCredits(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	creditsRepo *data.CreditsRepository,
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
		if creditsRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		ctx := r.Context()
		credit, err := creditsRepo.GetBalance(ctx, actor.AccountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		var fromDate, toDate *time.Time
		if v := r.URL.Query().Get("from"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				fromDate = &t
			}
		}
		if v := r.URL.Query().Get("to"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				next := t.AddDate(0, 0, 1)
				toDate = &next
			}
		}

		txns, err := creditsRepo.ListTransactionsWithDetails(ctx, actor.AccountID, 50, 0, fromDate, toDate)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, meCreditsResponse{
			Balance:      creditBalanceValue(credit),
			Transactions: toCreditTransactionDetailResponses(txns),
		})
	}
}

func adminCreditsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	creditsRepo *data.CreditsRepository,
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
		if creditsRepo == nil {
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

		accountIDStr := strings.TrimSpace(r.URL.Query().Get("account_id"))
		if accountIDStr == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "account_id is required", traceID, nil)
			return
		}
		accountID, err := uuid.Parse(accountIDStr)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
			return
		}

		ctx := r.Context()
		credit, err := creditsRepo.GetBalance(ctx, accountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
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

		txns, err := creditsRepo.ListTransactions(ctx, accountID, limit, offset)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		type adminCreditsResponse struct {
			AccountID        string                      `json:"account_id"`
			Balance      int64                       `json:"balance"`
			Transactions []creditTransactionResponse `json:"transactions"`
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, adminCreditsResponse{
			AccountID:        accountID.String(),
			Balance:      creditBalanceValue(credit),
			Transactions: toCreditTransactionResponses(txns),
		})
	}
}

func adminCreditsAdjust(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	creditsRepo *data.CreditsRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
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
		if creditsRepo == nil {
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

		var req adminAdjustRequest
		if err := httpkit.DecodeJSON(r, &req); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		req.Note = strings.TrimSpace(req.Note)
		if req.Note == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "note is required", traceID, nil)
			return
		}
		if req.Amount == 0 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "amount must not be zero", traceID, nil)
			return
		}

		accountID, err := uuid.Parse(req.AccountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
			return
		}

		ctx := r.Context()
		beforeCredit, err := creditsRepo.GetBalance(ctx, accountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		note := req.Note

		if req.Amount > 0 {
			err = creditsRepo.Add(ctx, accountID, req.Amount, "admin_adjustment", nil, nil, &note)
		} else {
			err = creditsRepo.Deduct(ctx, accountID, -req.Amount, "admin_adjustment", nil, nil, &note)
		}
		if err != nil {
			if _, ok := err.(data.InsufficientCreditsError); ok {
				httpkit.WriteError(w, nethttp.StatusConflict, "credits.insufficient", "insufficient credits", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		afterCredit, err := creditsRepo.GetBalance(ctx, accountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		beforeState := creditBalanceResponse{AccountID: accountID.String(), Balance: creditBalanceValue(beforeCredit)}
		afterState := creditBalanceResponse{AccountID: accountID.String(), Balance: creditBalanceValue(afterCredit)}
		if auditWriter != nil {
			auditWriter.WriteCreditsAdjusted(ctx, traceID, actor.UserID, accountID, req.Amount, note, beforeState, afterState)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, afterState)
	}
}

func toCreditTransactionResponses(txns []data.CreditTransaction) []creditTransactionResponse {
	result := make([]creditTransactionResponse, 0, len(txns))
	for _, t := range txns {
		resp := creditTransactionResponse{
			ID:        t.ID.String(),
			AccountID:     t.AccountID.String(),
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
		if len(t.Metadata) > 0 {
			raw := json.RawMessage(t.Metadata)
			resp.Metadata = &raw
		}
		result = append(result, resp)
	}
	return result
}

func toCreditTransactionDetailResponses(txns []data.CreditTransactionDetail) []creditTransactionResponse {
	result := make([]creditTransactionResponse, 0, len(txns))
	for _, t := range txns {
		resp := creditTransactionResponse{
			ID:          t.ID.String(),
			AccountID:       t.AccountID.String(),
			Amount:      t.Amount,
			Type:        t.Type,
			Note:        t.Note,
			ThreadTitle: t.ThreadTitle,
			CreatedAt:   t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if t.ReferenceType != nil {
			resp.ReferenceType = t.ReferenceType
		}
		if t.ReferenceID != nil {
			s := t.ReferenceID.String()
			resp.ReferenceID = &s
		}
		if len(t.Metadata) > 0 {
			raw := json.RawMessage(t.Metadata)
			resp.Metadata = &raw
		}
		result = append(result, resp)
	}
	return result
}

func adminCreditsBulkAdjust(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	creditsRepo *data.CreditsRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	type reqBody struct {
		Amount int64  `json:"amount"`
		Note   string `json:"note"`
	}
	type respBody struct {
		Affected int64 `json:"affected"`
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
		if creditsRepo == nil {
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

		var req reqBody
		if err := httpkit.DecodeJSON(r, &req); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid request body", traceID, nil)
			return
		}
		req.Note = strings.TrimSpace(req.Note)
		if req.Amount == 0 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "amount must not be zero", traceID, nil)
			return
		}
		if req.Note == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "note is required", traceID, nil)
			return
		}

		affected, err := creditsRepo.BulkAdjust(r.Context(), req.Amount, req.Note)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if auditWriter != nil {
			auditWriter.WriteCreditsBulkAdjusted(r.Context(), traceID, actor.UserID, req.Amount, req.Note, affected)
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, respBody{Affected: affected})
	}
}

func adminCreditsResetAll(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	creditsRepo *data.CreditsRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	type reqBody struct {
		Note string `json:"note"`
	}
	type respBody struct {
		Affected int64 `json:"affected"`
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
		if creditsRepo == nil {
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

		var req reqBody
		if err := httpkit.DecodeJSON(r, &req); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "invalid request body", traceID, nil)
			return
		}
		req.Note = strings.TrimSpace(req.Note)
		if req.Note == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "note is required", traceID, nil)
			return
		}

		affected, err := creditsRepo.ResetAll(r.Context(), req.Note)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if auditWriter != nil {
			auditWriter.WriteCreditsResetAll(r.Context(), traceID, actor.UserID, req.Note, affected)
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, respBody{Affected: affected})
	}
}

func creditBalanceValue(credit *data.Credit) int64 {
	if credit == nil {
		return 0
	}
	return credit.Balance
}
