package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type createFeedbackRequest struct {
	Feedback string `json:"feedback"`
}

func meFeedback(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	reportRepo *data.ThreadReportRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
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

		var body createFeedbackRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		feedback := strings.TrimSpace(body.Feedback)
		if feedback == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "feedback must not be empty", traceID, nil)
			return
		}
		if len(feedback) > 2000 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "feedback too long", traceID, nil)
			return
		}

		report, err := reportRepo.CreateSuggestion(r.Context(), actor.UserID, feedback)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, reportResponse{
			ID:        report.ID.String(),
			CreatedAt: report.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
}
