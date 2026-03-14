package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"arkloop/services/sandbox/internal/acp"
	"arkloop/services/sandbox/internal/logging"
)

func handleACPStart(svc acp.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "acp.not_configured", "acp service not configured")
			return
		}
		var req acp.StartACPAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.AccountID = strings.TrimSpace(req.AccountID)
		req.Tier = strings.TrimSpace(req.Tier)
		req.Cwd = strings.TrimSpace(req.Cwd)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if len(req.Command) == 0 {
			writeError(w, http.StatusBadRequest, "acp.missing_command", "command is required")
			return
		}

		resp, err := svc.StartACPAgent(r.Context(), req)
		if err != nil {
			writeACPError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleACPWrite(svc acp.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "acp.not_configured", "acp service not configured")
			return
		}
		var req acp.WriteACPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.AccountID = strings.TrimSpace(req.AccountID)
		req.ProcessID = strings.TrimSpace(req.ProcessID)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if req.ProcessID == "" {
			writeError(w, http.StatusBadRequest, "acp.missing_process_id", "process_id is required")
			return
		}

		resp, err := svc.WriteACP(r.Context(), req)
		if err != nil {
			writeACPError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleACPRead(svc acp.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "acp.not_configured", "acp service not configured")
			return
		}
		var req acp.ReadACPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.AccountID = strings.TrimSpace(req.AccountID)
		req.ProcessID = strings.TrimSpace(req.ProcessID)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if req.ProcessID == "" {
			writeError(w, http.StatusBadRequest, "acp.missing_process_id", "process_id is required")
			return
		}

		resp, err := svc.ReadACP(r.Context(), req)
		if err != nil {
			writeACPError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleACPStop(svc acp.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "acp.not_configured", "acp service not configured")
			return
		}
		var req acp.StopACPAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.AccountID = strings.TrimSpace(req.AccountID)
		req.ProcessID = strings.TrimSpace(req.ProcessID)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if req.ProcessID == "" {
			writeError(w, http.StatusBadRequest, "acp.missing_process_id", "process_id is required")
			return
		}

		resp, err := svc.StopACPAgent(r.Context(), req)
		if err != nil {
			writeACPError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleACPWait(svc acp.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "acp.not_configured", "acp service not configured")
			return
		}
		var req acp.WaitACPAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.AccountID = strings.TrimSpace(req.AccountID)
		req.ProcessID = strings.TrimSpace(req.ProcessID)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if req.ProcessID == "" {
			writeError(w, http.StatusBadRequest, "acp.missing_process_id", "process_id is required")
			return
		}

		resp, err := svc.WaitACPAgent(r.Context(), req)
		if err != nil {
			writeACPError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleACPStatus(svc acp.Service, _ *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "acp.not_configured", "acp service not configured")
			return
		}
		var req acp.StatusACPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.AccountID = strings.TrimSpace(req.AccountID)
		req.ProcessID = strings.TrimSpace(req.ProcessID)
		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if req.ProcessID == "" {
			writeError(w, http.StatusBadRequest, "acp.missing_process_id", "process_id is required")
			return
		}

		resp, err := svc.StatusACP(r.Context(), req)
		if err != nil {
			writeACPError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func writeACPError(w http.ResponseWriter, err error) {
	if acpErr, ok := err.(*acp.Error); ok {
		status := acpErr.HTTPStatus
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeError(w, status, acpErr.Code, acpErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "acp.internal_error", err.Error())
}
