package http

import (
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
	membershipRepo *data.OrgMembershipRepository,
	reportRepo *data.ThreadReportRepository,
	apiKeysRepo *data.APIKeysRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if reportRepo == nil {
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

		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		offset := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid offset", traceID, nil)
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
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid report_id", traceID, nil)
				return
			}
			params.ReportID = &parsed
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("thread_id")); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid thread_id", traceID, nil)
				return
			}
			params.ThreadID = &parsed
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("reporter_id")); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid reporter_id", traceID, nil)
				return
			}
			params.ReporterID = &parsed
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
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid since: must be RFC3339", traceID, nil)
				return
			}
			params.Since = &since
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
			until, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid until: must be RFC3339", traceID, nil)
				return
			}
			params.Until = &until
		}
		if params.Since != nil && params.Until != nil && params.Since.After(*params.Until) {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "since must be <= until", traceID, nil)
			return
		}

		rows, total, err := reportRepo.List(r.Context(), params)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		items := make([]adminReportItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, adminReportItem{
				ID:            row.ID.String(),
				ThreadID:      row.ThreadID.String(),
				ReporterID:    row.ReporterID.String(),
				ReporterEmail: row.ReporterEmail,
				Categories:    row.Categories,
				Feedback:      row.Feedback,
				CreatedAt:     row.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
		}

		writeJSON(w, traceID, nethttp.StatusOK, adminReportsResponse{
			Data:  items,
			Total: total,
		})
	}
}
