package billingapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strconv"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func meUsage(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usageRepo *data.UsageRepository,
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
		if usageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		now := time.Now().UTC()
		year := now.Year()
		month := int(now.Month())

		if y := r.URL.Query().Get("year"); y != "" {
			parsed, parseErr := strconv.Atoi(y)
			if parseErr != nil || parsed < 2000 || parsed > 2100 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid year", traceID, nil)
				return
			}
			year = parsed
		}
		if m := r.URL.Query().Get("month"); m != "" {
			parsed, parseErr := strconv.Atoi(m)
			if parseErr != nil || parsed < 1 || parsed > 12 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "month must be 1-12", traceID, nil)
				return
			}
			month = parsed
		}

		summary, err := usageRepo.GetMonthlyUsage(r.Context(), actor.AccountID, year, month)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, usageSummaryResponse{
			AccountID:             summary.AccountID.String(),
			Year:              summary.Year,
			Month:             summary.Month,
			TotalInputTokens:  summary.TotalInputTokens,
			TotalOutputTokens: summary.TotalOutputTokens,
			TotalCostUSD:      summary.TotalCostUSD,
			RecordCount:       summary.RecordCount,
		})
	}
}

func meDailyUsage(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usageRepo *data.UsageRepository,
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
		if usageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		startDate, endDate, ok := parseDateRange(w, r, traceID)
		if !ok {
			return
		}

		rows, err := usageRepo.GetDailyUsage(r.Context(), actor.AccountID, startDate, endDate)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		items := make([]dailyUsageItem, len(rows))
		for i, row := range rows {
			items[i] = dailyUsageItem{
				Date:         row.Date.Format("2006-01-02"),
				InputTokens:  row.InputTokens,
				OutputTokens: row.OutputTokens,
				CostUSD:      row.CostUSD,
				RecordCount:  row.RecordCount,
			}
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, items)
	}
}

func meUsageByModel(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usageRepo *data.UsageRepository,
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
		if usageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		now := time.Now().UTC()
		year := now.Year()
		month := int(now.Month())

		if y := r.URL.Query().Get("year"); y != "" {
			parsed, parseErr := strconv.Atoi(y)
			if parseErr != nil || parsed < 2000 || parsed > 2100 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid year", traceID, nil)
				return
			}
			year = parsed
		}
		if m := r.URL.Query().Get("month"); m != "" {
			parsed, parseErr := strconv.Atoi(m)
			if parseErr != nil || parsed < 1 || parsed > 12 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "month must be 1-12", traceID, nil)
				return
			}
			month = parsed
		}

		rows, err := usageRepo.GetUsageByModel(r.Context(), actor.AccountID, year, month)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		items := make([]modelUsageItem, len(rows))
		for i, row := range rows {
			items[i] = modelUsageItem{
				Model:        row.Model,
				InputTokens:  row.InputTokens,
				OutputTokens: row.OutputTokens,
				CostUSD:      row.CostUSD,
				RecordCount:  row.RecordCount,
			}
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, items)
	}
}
