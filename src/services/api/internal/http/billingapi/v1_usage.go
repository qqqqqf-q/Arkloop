package billingapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strconv"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type dailyUsageItem struct {
	Date         string  `json:"date"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	RecordCount  int64   `json:"record_count"`
}

type modelUsageItem struct {
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	RecordCount  int64   `json:"record_count"`
}

type usageSummaryResponse struct {
	AccountID             string  `json:"account_id"`
	Year              int     `json:"year"`
	Month             int     `json:"month"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCacheCreationTokens int64 `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int64 `json:"total_cache_read_tokens"`
	TotalCachedTokens        int64 `json:"total_cached_tokens"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	RecordCount       int64   `json:"record_count"`
}

func accountUsageEntry(
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

		rawID := r.PathValue("id")
		accountID, err := uuid.Parse(rawID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account id", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		// 非平台管理员只能查自己 account 的用量。
		if !actor.HasPermission(auth.PermPlatformAdmin) {
			if !httpkit.RequirePerm(actor, auth.PermDataUsageRead, w, traceID) {
				return
			}
			if actor.AccountID != accountID {
				httpkit.WriteError(w, nethttp.StatusForbidden, "forbidden", "forbidden", traceID, nil)
				return
			}
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

		summary, err := usageRepo.GetMonthlyUsage(r.Context(), accountID, year, month)
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
			TotalCacheCreationTokens: summary.TotalCacheCreationTokens,
			TotalCacheReadTokens:     summary.TotalCacheReadTokens,
			TotalCachedTokens:        summary.TotalCachedTokens,
			TotalCostUSD:      summary.TotalCostUSD,
			RecordCount:       summary.RecordCount,
		})
	}
}

// resolveAccountUsageActor 提取 account ID 并校验用量读取权限。
func resolveAccountUsageActor(
	w nethttp.ResponseWriter, r *nethttp.Request, traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usageRepo *data.UsageRepository,
	apiKeysRepo *data.APIKeysRepository,
) (uuid.UUID, bool) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return uuid.Nil, false
	}
	if usageRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return uuid.Nil, false
	}

	rawID := r.PathValue("id")
	accountID, err := uuid.Parse(rawID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid account id", traceID, nil)
		return uuid.Nil, false
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return uuid.Nil, false
	}

	if !actor.HasPermission(auth.PermPlatformAdmin) {
		if !httpkit.RequirePerm(actor, auth.PermDataUsageRead, w, traceID) {
			return uuid.Nil, false
		}
		if actor.AccountID != accountID {
			httpkit.WriteError(w, nethttp.StatusForbidden, "forbidden", "forbidden", traceID, nil)
			return uuid.Nil, false
		}
	}
	return accountID, true
}

func accountDailyUsage(
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

		accountID, ok := resolveAccountUsageActor(w, r, traceID, authService, membershipRepo, usageRepo, apiKeysRepo)
		if !ok {
			return
		}

		startDate, endDate, ok := parseDateRange(w, r, traceID)
		if !ok {
			return
		}

		rows, err := usageRepo.GetDailyUsage(r.Context(), accountID, startDate, endDate)
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

func accountUsageByModel(
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

		accountID, ok := resolveAccountUsageActor(w, r, traceID, authService, membershipRepo, usageRepo, apiKeysRepo)
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

		rows, err := usageRepo.GetUsageByModel(r.Context(), accountID, year, month)
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
				CacheCreationTokens: row.CacheCreationTokens,
				CacheReadTokens:     row.CacheReadTokens,
				CachedTokens:        row.CachedTokens,
				CostUSD:      row.CostUSD,
				RecordCount:  row.RecordCount,
			}
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, items)
	}
}

func adminGlobalDailyUsage(
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
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		startDate, endDate, ok := parseDateRange(w, r, traceID)
		if !ok {
			return
		}

		rows, err := usageRepo.GetGlobalDailyUsage(r.Context(), startDate, endDate)
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

// resolveAdminUsageActor 校验 admin 权限并检查 repo 可用性。
func resolveAdminUsageActor(
	w nethttp.ResponseWriter, r *nethttp.Request, traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	usageRepo *data.UsageRepository,
	apiKeysRepo *data.APIKeysRepository,
) bool {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return false
	}
	if usageRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return false
	}
	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return false
	}
	return httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID)
}

func adminGlobalUsageSummary(
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

		if !resolveAdminUsageActor(w, r, traceID, authService, membershipRepo, usageRepo, apiKeysRepo) {
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

		summary, err := usageRepo.GetGlobalMonthlyUsage(r.Context(), year, month)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, usageSummaryResponse{
			Year:              summary.Year,
			Month:             summary.Month,
			TotalInputTokens:  summary.TotalInputTokens,
			TotalOutputTokens: summary.TotalOutputTokens,
			TotalCacheCreationTokens: summary.TotalCacheCreationTokens,
			TotalCacheReadTokens:     summary.TotalCacheReadTokens,
			TotalCachedTokens:        summary.TotalCachedTokens,
			TotalCostUSD:      summary.TotalCostUSD,
			RecordCount:       summary.RecordCount,
		})
	}
}

func adminGlobalUsageByModel(
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

		if !resolveAdminUsageActor(w, r, traceID, authService, membershipRepo, usageRepo, apiKeysRepo) {
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

		rows, err := usageRepo.GetGlobalUsageByModel(r.Context(), year, month)
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
				CacheCreationTokens: row.CacheCreationTokens,
				CacheReadTokens:     row.CacheReadTokens,
				CachedTokens:        row.CachedTokens,
				CostUSD:      row.CostUSD,
				RecordCount:  row.RecordCount,
			}
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, items)
	}
}
func parseDateRange(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (time.Time, time.Time, bool) {
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	if startStr == "" || endStr == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "start and end are required (YYYY-MM-DD)", traceID, nil)
		return time.Time{}, time.Time{}, false
	}

	startDate, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid start date", traceID, nil)
		return time.Time{}, time.Time{}, false
	}

	endDate, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid end date", traceID, nil)
		return time.Time{}, time.Time{}, false
	}

	// end 取到当天结束
	endDate = endDate.AddDate(0, 0, 1)

	return startDate, endDate, true
}
