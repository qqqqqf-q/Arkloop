package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/shell"
)

func handleSessionInfo(shellSvc shell.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if shellSvc == nil {
			writeError(w, http.StatusServiceUnavailable, shell.CodeSessionNotFound, "shell service not configured")
			return
		}

		tail := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
		if !strings.HasSuffix(tail, "/transcript") && !strings.HasSuffix(tail, "/output_deltas") {
			writeError(w, http.StatusNotFound, "sandbox.session_not_found", "session not found")
			return
		}
		isTranscript := strings.HasSuffix(tail, "/transcript")
		id := strings.TrimSuffix(tail, "/transcript")
		id = strings.TrimSuffix(id, "/output_deltas")
		id = strings.Trim(id, "/")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session id is required")
			return
		}

		accountID := strings.TrimSpace(r.Header.Get("X-Account-ID"))
		if isTranscript {
			resp, err := shellSvc.DebugSnapshot(r.Context(), id, accountID)
			if err != nil {
				if shellErr, ok := err.(*shell.Error); ok {
					writeError(w, shellErr.HTTPStatus, shellErr.Code, shellErr.Message)
					return
				}
				writeError(w, http.StatusInternalServerError, "sandbox.shell_error", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, resp)
		} else {
			resp, err := shellSvc.ReadOutputDeltas(r.Context(), id, accountID)
			if err != nil {
				if shellErr, ok := err.(*shell.Error); ok {
					writeError(w, shellErr.HTTPStatus, shellErr.Code, shellErr.Message)
					return
				}
				writeError(w, http.StatusInternalServerError, "sandbox.shell_error", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, resp)
		}
	}
}

func handleForkSession(shellSvc shell.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if shellSvc == nil {
			writeError(w, http.StatusServiceUnavailable, shell.CodeSessionNotFound, "shell service not configured")
			return
		}
		var req shell.ForkSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.AccountID = strings.TrimSpace(req.AccountID)
		req.FromSessionID = strings.TrimSpace(req.FromSessionID)
		req.ToSessionID = strings.TrimSpace(req.ToSessionID)
		if req.AccountID == "" {
			req.AccountID = strings.TrimSpace(r.Header.Get("X-Account-ID"))
		}
		if req.FromSessionID == "" || req.ToSessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "from_session_id and to_session_id are required")
			return
		}
		resp, err := shellSvc.ForkSession(r.Context(), req)
		if err != nil {
			writeShellError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleDeleteSession(mgr *session.Manager, shellSvc shell.Service, logger *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
		id = strings.TrimSpace(id)
		if id == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session id is required")
			return
		}

		accountID := strings.TrimSpace(r.Header.Get("X-Account-ID"))
		if shellSvc != nil {
			if err := shellSvc.Close(r.Context(), id, accountID); err == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			} else if shellErr, ok := err.(*shell.Error); ok && shellErr.Code == shell.CodeAccountMismatch {
				writeError(w, http.StatusForbidden, shellErr.Code, shellErr.Message)
				return
			} else if shellErr, ok := err.(*shell.Error); ok && shellErr.Code != shell.CodeSessionNotFound {
				writeError(w, http.StatusConflict, shellErr.Code, shellErr.Message)
				return
			}
		}

		if err := mgr.Delete(r.Context(), id, accountID); err != nil {
			if strings.Contains(err.Error(), "account mismatch") {
				writeError(w, http.StatusForbidden, "sandbox.account_mismatch", "session belongs to another account")
				return
			}
			logger.Warn("delete session not found", logging.LogFields{SessionID: &id}, nil)
			writeError(w, http.StatusNotFound, "sandbox.session_not_found", err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
