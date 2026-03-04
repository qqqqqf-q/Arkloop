package http

import (
	"net/http"
	"strings"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

func handleDeleteSession(mgr *session.Manager, logger *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 路由: DELETE /v1/sessions/{id}
		id := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
		id = strings.TrimSpace(id)
		if id == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session id is required")
			return
		}

		orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))

		if err := mgr.Delete(r.Context(), id, orgID); err != nil {
			if strings.Contains(err.Error(), "org mismatch") {
				writeError(w, http.StatusForbidden, "sandbox.org_mismatch", "session belongs to another org")
				return
			}
			logger.Warn("delete session not found", logging.LogFields{SessionID: &id}, nil)
			writeError(w, http.StatusNotFound, "sandbox.session_not_found", err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
