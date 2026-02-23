package http

import (
	"strconv"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type usageSummaryResponse struct {
	OrgID             string  `json:"org_id"`
	Year              int     `json:"year"`
	Month             int     `json:"month"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	RecordCount       int64   `json:"record_count"`
}

func orgUsageEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	usageRepo *data.UsageRepository,
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
		if usageRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		rawID := r.PathValue("id")
		orgID, err := uuid.Parse(rawID)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org id", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		// 非平台管理员只能查自己 org 的用量。
		if !actor.HasPermission(auth.PermPlatformAdmin) {
			if !requirePerm(actor, auth.PermDataUsageRead, w, traceID) {
				return
			}
			if actor.OrgID != orgID {
				WriteError(w, nethttp.StatusForbidden, "forbidden", "forbidden", traceID, nil)
				return
			}
		}

		now := time.Now().UTC()
		year := now.Year()
		month := int(now.Month())

		if y := r.URL.Query().Get("year"); y != "" {
			parsed, parseErr := strconv.Atoi(y)
			if parseErr != nil || parsed < 2000 || parsed > 2100 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid year", traceID, nil)
				return
			}
			year = parsed
		}
		if m := r.URL.Query().Get("month"); m != "" {
			parsed, parseErr := strconv.Atoi(m)
			if parseErr != nil || parsed < 1 || parsed > 12 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "month must be 1-12", traceID, nil)
				return
			}
			month = parsed
		}

		summary, err := usageRepo.GetMonthlyUsage(r.Context(), orgID, year, month)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusOK, usageSummaryResponse{
			OrgID:             summary.OrgID.String(),
			Year:              summary.Year,
			Month:             summary.Month,
			TotalInputTokens:  summary.TotalInputTokens,
			TotalOutputTokens: summary.TotalOutputTokens,
			TotalCostUSD:      summary.TotalCostUSD,
			RecordCount:       summary.RecordCount,
		})
	}
}
