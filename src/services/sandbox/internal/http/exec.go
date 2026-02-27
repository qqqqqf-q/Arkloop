package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

// ExecRequest 是 POST /v1/exec 的请求体。
type ExecRequest struct {
	SessionID string `json:"session_id"`
	Tier      string `json:"tier"`       // "lite" | "pro" | "ultra"
	Language  string `json:"language"`   // "python" | "shell"
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"` // 0 表示使用服务端默认值（30s）
}

// ExecResponse 是 POST /v1/exec 的响应体。
type ExecResponse struct {
	SessionID  string `json:"session_id"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

func handleExec(mgr *session.Manager, logger *logging.JSONLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ExecRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_request", "invalid JSON body")
			return
		}

		req.SessionID = strings.TrimSpace(req.SessionID)
		req.Tier = strings.TrimSpace(req.Tier)
		req.Language = strings.TrimSpace(req.Language)

		if req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_session_id", "session_id is required")
			return
		}
		if req.Language != "python" && req.Language != "shell" {
			writeError(w, http.StatusBadRequest, "sandbox.invalid_language", "language must be python or shell")
			return
		}
		if req.Tier == "" {
			req.Tier = "lite"
		}
		if req.TimeoutMs <= 0 {
			req.TimeoutMs = 30_000
		}
		if req.TimeoutMs > 300_000 {
			writeError(w, http.StatusBadRequest, "sandbox.timeout_too_large", "timeout_ms must not exceed 300000")
			return
		}

		sid := req.SessionID
		sn, err := mgr.GetOrCreate(r.Context(), sid, req.Tier)
		if err != nil {
			logger.Error("get/create session failed", logging.LogFields{SessionID: &sid}, map[string]any{"error": err.Error()})
			writeError(w, http.StatusInternalServerError, "sandbox.session_error", err.Error())
			return
		}

		start := time.Now()
		result, err := sn.Exec(r.Context(), session.ExecJob{
			Language:  req.Language,
			Code:      req.Code,
			TimeoutMs: req.TimeoutMs,
		})
		elapsed := time.Since(start).Milliseconds()

		if err != nil {
			logger.Error("exec failed", logging.LogFields{SessionID: &sid}, map[string]any{"error": err.Error()})
			writeError(w, http.StatusInternalServerError, "sandbox.exec_error", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, ExecResponse{
			SessionID:  sid,
			Stdout:     result.Stdout,
			Stderr:     result.Stderr,
			ExitCode:   result.ExitCode,
			DurationMs: elapsed,
		})
	}
}
