package billingapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type subscriptionResponse struct {
	ID                 string  `json:"id"`
	AccountID              string  `json:"account_id"`
	PlanID             string  `json:"plan_id"`
	Status             string  `json:"status"`
	CurrentPeriodStart string  `json:"current_period_start"`
	CurrentPeriodEnd   string  `json:"current_period_end"`
	CancelledAt        *string `json:"cancelled_at,omitempty"`
	CreatedAt          string  `json:"created_at"`
}

type createSubscriptionRequest struct {
	AccountID              string `json:"account_id"`
	PlanID             string `json:"plan_id"`
	CurrentPeriodStart string `json:"current_period_start"`
	CurrentPeriodEnd   string `json:"current_period_end"`
}

func subscriptionsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	subscriptionRepo *data.SubscriptionRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createSubscription(w, r, authService, membershipRepo, subscriptionRepo, apiKeysRepo)
		case nethttp.MethodGet:
			listOrGetSubscription(w, r, authService, membershipRepo, subscriptionRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func subscriptionEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	subscriptionRepo *data.SubscriptionRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		subID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid subscription id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getSubscription(w, r, traceID, subID, authService, membershipRepo, subscriptionRepo, apiKeysRepo)
		case nethttp.MethodDelete:
			cancelSubscription(w, r, traceID, subID, authService, membershipRepo, subscriptionRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createSubscription(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	subscriptionRepo *data.SubscriptionRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if subscriptionRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformSubscriptionsManage, w, traceID) {
		return
	}

	var req createSubscriptionRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	accountID, err := uuid.Parse(strings.TrimSpace(req.AccountID))
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account_id", traceID, nil)
		return
	}
	planID, err := uuid.Parse(strings.TrimSpace(req.PlanID))
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid plan_id", traceID, nil)
		return
	}

	periodStart, err := time.Parse(time.RFC3339, strings.TrimSpace(req.CurrentPeriodStart))
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "current_period_start must be RFC3339", traceID, nil)
		return
	}
	periodEnd, err := time.Parse(time.RFC3339, strings.TrimSpace(req.CurrentPeriodEnd))
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "current_period_end must be RFC3339", traceID, nil)
		return
	}

	sub, err := subscriptionRepo.Create(r.Context(), accountID, planID, periodStart, periodEnd)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusConflict, "subscriptions.conflict", err.Error(), traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toSubscriptionResponse(sub))
}

// listOrGetSubscription: platform_admin 无 query 时返回全部; 普通成员返回自身 account 的订阅。
func listOrGetSubscription(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	subscriptionRepo *data.SubscriptionRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if subscriptionRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	// platform_admin 查看全部
	if actor.HasPermission(auth.PermPlatformSubscriptionsManage) {
		subs, err := subscriptionRepo.List(r.Context())
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		resp := make([]subscriptionResponse, 0, len(subs))
		for _, s := range subs {
			resp = append(resp, toSubscriptionResponse(s))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
		return
	}

	// 普通成员: 返回自己 account 的 active subscription
	if !httpkit.RequirePerm(actor, auth.PermDataSubscriptionsRead, w, traceID) {
		return
	}
	sub, err := subscriptionRepo.GetActiveByAccountID(r.Context(), actor.AccountID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if sub == nil {
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, []subscriptionResponse{})
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, []subscriptionResponse{toSubscriptionResponse(*sub)})
}

func getSubscription(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	subID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	subscriptionRepo *data.SubscriptionRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if subscriptionRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	sub, err := subscriptionRepo.GetByID(r.Context(), subID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if sub == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "subscriptions.not_found", "subscription not found", traceID, nil)
		return
	}

	// 非 platform_admin 只能查看自己 account 的订阅
	if !actor.HasPermission(auth.PermPlatformSubscriptionsManage) && sub.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "subscriptions.not_found", "subscription not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toSubscriptionResponse(*sub))
}

func cancelSubscription(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	subID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	subscriptionRepo *data.SubscriptionRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if subscriptionRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformSubscriptionsManage, w, traceID) {
		return
	}

	cancelled, err := subscriptionRepo.Cancel(r.Context(), subID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if cancelled == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "subscriptions.not_found", "subscription not found or already cancelled", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toSubscriptionResponse(*cancelled))
}

func toSubscriptionResponse(s data.Subscription) subscriptionResponse {
	resp := subscriptionResponse{
		ID:                 s.ID.String(),
		AccountID:              s.AccountID.String(),
		PlanID:             s.PlanID.String(),
		Status:             s.Status,
		CurrentPeriodStart: s.CurrentPeriodStart.UTC().Format(time.RFC3339Nano),
		CurrentPeriodEnd:   s.CurrentPeriodEnd.UTC().Format(time.RFC3339Nano),
		CreatedAt:          s.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if s.CancelledAt != nil {
		t := s.CancelledAt.UTC().Format(time.RFC3339Nano)
		resp.CancelledAt = &t
	}
	return resp
}
