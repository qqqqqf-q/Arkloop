package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type adminReportItem struct {
	ID            string   `json:"id"`
	ThreadID      string   `json:"thread_id"`
	ReporterID    string   `json:"reporter_id"`
	ReporterEmail string   `json:"reporter_email"`
	Categories    []string `json:"categories"`
	Feedback      *string  `json:"feedback"`
	CreatedAt     string   `json:"created_at"`
}

type adminReportsResponse struct {
	Data  []adminReportItem `json:"data"`
	Total int               `json:"total"`
}

func adminReportsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	reportRepo *data.ThreadReportRepository,
	apiKeysRepo *data.APIKeysRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if reportRepo == nil {
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

		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		limit, ok := httpkit.ParseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		offset := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid offset", traceID, nil)
				return
			}
			offset = parsed
		}

		params := data.ThreadReportListParams{
			Limit:  limit,
			Offset: offset,
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("report_id")); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err == nil {
				params.ReportID = &parsed
			} else {
				if !uuidPrefixRegex.MatchString(raw) {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid report_id", traceID, nil)
					return
				}
				params.ReportIDPrefix = &raw
			}
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("thread_id")); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err == nil {
				params.ThreadID = &parsed
			} else {
				if !uuidPrefixRegex.MatchString(raw) {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid thread_id", traceID, nil)
					return
				}
				params.ThreadIDPrefix = &raw
			}
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("reporter_id")); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err == nil {
				params.ReporterID = &parsed
			} else {
				if !uuidPrefixRegex.MatchString(raw) {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid reporter_id", traceID, nil)
					return
				}
				params.ReporterIDPrefix = &raw
			}
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("reporter_email")); raw != "" {
			params.ReporterEmail = &raw
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("category")); raw != "" {
			params.Category = &raw
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("feedback")); raw != "" {
			params.FeedbackKeyword = &raw
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
			since, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid since: must be RFC3339", traceID, nil)
				return
			}
			params.Since = &since
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
			until, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid until: must be RFC3339", traceID, nil)
				return
			}
			params.Until = &until
		}
		if params.Since != nil && params.Until != nil && params.Since.After(*params.Until) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "since must be <= until", traceID, nil)
			return
		}

		rows, total, err := reportRepo.List(r.Context(), params)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		items := make([]adminReportItem, 0, len(rows))
		for _, row := range rows {
			threadID := ""
			if row.ThreadID != nil {
				threadID = row.ThreadID.String()
			}
			items = append(items, adminReportItem{
				ID:            row.ID.String(),
				ThreadID:      threadID,
				ReporterID:    row.ReporterID.String(),
				ReporterEmail: row.ReporterEmail,
				Categories:    row.Categories,
				Feedback:      row.Feedback,
				CreatedAt:     row.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, adminReportsResponse{
			Data:  items,
			Total: total,
		})
	}
}
