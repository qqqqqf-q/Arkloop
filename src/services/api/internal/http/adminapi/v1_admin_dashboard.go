package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type dashboardResponse struct {
	TotalUsers        int64   `json:"total_users"`
	ActiveUsers30d    int64   `json:"active_users_30d"`
	TotalRuns         int64   `json:"total_runs"`
	RunsToday         int64   `json:"runs_today"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	ActiveAccounts        int64   `json:"active_accounts"`
}

func adminDashboard(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usersRepo *data.UserRepository,
	runEventRepo *data.RunEventRepository,
	usageRepo *data.UsageRepository,
	accountRepo *data.AccountRepository,
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

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if usersRepo == nil || runEventRepo == nil || usageRepo == nil || accountRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		ctx := r.Context()

		totalUsers, err := usersRepo.CountAll(ctx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		since30d := time.Now().UTC().AddDate(0, 0, -30)
		activeUsers30d, err := usersRepo.CountActiveSince(ctx, since30d)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		totalRuns, err := runEventRepo.CountAll(ctx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		now := time.Now().UTC()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		runsToday, err := runEventRepo.CountSince(ctx, todayStart)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		usageSummary, err := usageRepo.GetGlobalSummary(ctx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		activeAccounts, err := accountRepo.CountActive(ctx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, dashboardResponse{
			TotalUsers:        totalUsers,
			ActiveUsers30d:    activeUsers30d,
			TotalRuns:         totalRuns,
			RunsToday:         runsToday,
			TotalInputTokens:  usageSummary.TotalInputTokens,
			TotalOutputTokens: usageSummary.TotalOutputTokens,
			TotalCostUSD:      usageSummary.TotalCostUSD,
			ActiveAccounts:        activeAccounts,
		})
	}
}
