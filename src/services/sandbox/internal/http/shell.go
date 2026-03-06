package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/shell"
)

func handleExecCommand(svc shell.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return handleExecAction(svc, func(ctx context.Context, req shell.ExecCommandRequest, svc shell.Service) (*shell.Response, error) {
		return svc.ExecCommand(ctx, req)
	})
}

func handleWriteStdin(svc shell.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return handleWriteAction(svc, func(ctx context.Context, req shell.WriteStdinRequest, svc shell.Service) (*shell.Response, error) {
		return svc.WriteStdin(ctx, req)
	})
}

type execActionFunc func(ctx context.Context, req shell.ExecCommandRequest, svc shell.Service) (*shell.Response, error)

type writeActionFunc func(ctx context.Context, req shell.WriteStdinRequest, svc shell.Service) (*shell.Response, error)

func handleExecAction(svc shell.Service, fn execActionFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, shell.CodeSessionNotFound, "shell service not configured")
			return
		}

		var req shell.ExecCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.OrgID = strings.TrimSpace(req.OrgID)
		req.Tier = strings.TrimSpace(req.Tier)
		req.Cwd = strings.TrimSpace(req.Cwd)
		req.Command = strings.TrimSpace(req.Command)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if req.Command == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_command", "command is required")
			return
		}
		if req.Tier == "" {
			req.Tier = "lite"
		}
		if err := shell.ValidateTimeoutMs(req.TimeoutMs); err != nil {
			writeShellError(w, err)
			return
		}

		resp, err := fn(r.Context(), req, svc)
		if err != nil {
			writeShellError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleWriteAction(svc shell.Service, fn writeActionFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, shell.CodeSessionNotFound, "shell service not configured")
			return
		}

		var req shell.WriteStdinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.OrgID = strings.TrimSpace(req.OrgID)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}

		resp, err := fn(r.Context(), req, svc)
		if err != nil {
			writeShellError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func writeShellError(w http.ResponseWriter, err error) {
	if shellErr, ok := err.(*shell.Error); ok {
		status := shellErr.HTTPStatus
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeError(w, status, shellErr.Code, shellErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "sandbox.shell_error", err.Error())
}
