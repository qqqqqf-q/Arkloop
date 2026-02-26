package http

import (
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type emailStatusResponse struct {
	Configured bool   `json:"configured"`
	From       string `json:"from,omitempty"`
	Provider   string `json:"provider"`
}

type adminEmailTestRequest struct {
	To string `json:"to"`
}

func adminEmailStatus(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	emailFrom string,
	emailConfigured bool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		provider := "noop"
		if emailConfigured {
			provider = "smtp"
		}

		writeJSON(w, traceID, nethttp.StatusOK, emailStatusResponse{
			Configured: emailConfigured,
			From:       emailFrom,
			Provider:   provider,
		})
	}
}

func adminEmailTest(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	jobRepo *data.JobRepository,
	emailConfigured bool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		if !emailConfigured {
			WriteError(w, nethttp.StatusServiceUnavailable, "email.not_configured", "email not configured", traceID, nil)
			return
		}

		var body adminEmailTestRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.To = strings.TrimSpace(body.To)
		if body.To == "" || !strings.Contains(body.To, "@") {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "to must be a valid email address", traceID, nil)
			return
		}

		html := "<p>This is a test email from Arkloop.</p>"
		text := "This is a test email from Arkloop."
		if _, err := jobRepo.EnqueueEmail(r.Context(), body.To, "Arkloop test email", html, text); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}
