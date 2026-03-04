package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/shared/objectstore"
)

// ExecRequest 是 POST /v1/exec 的请求体。
type ExecRequest struct {
	SessionID string `json:"session_id"`
	OrgID     string `json:"org_id"`
	Tier      string `json:"tier"`       // "lite" | "pro" | "ultra"
	Language  string `json:"language"`   // "python" | "shell"
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"` // 0 表示使用服务端默认值（30s）
}

// ArtifactRef 描述一个已上传到对象存储的执行产物。
type ArtifactRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

// ExecResponse 是 POST /v1/exec 的响应体。
type ExecResponse struct {
	SessionID  string        `json:"session_id"`
	Stdout     string        `json:"stdout"`
	Stderr     string        `json:"stderr"`
	ExitCode   int           `json:"exit_code"`
	DurationMs int64         `json:"duration_ms"`
	Artifacts  []ArtifactRef `json:"artifacts,omitempty"`
}

func handleExec(mgr *session.Manager, artifactStore *objectstore.Store, logger *logging.JSONLogger) http.HandlerFunc {
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
		if strings.TrimSpace(req.Code) == "" {
			writeError(w, http.StatusBadRequest, "sandbox.missing_code", "code is required")
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
		orgID := strings.TrimSpace(req.OrgID)
		sn, err := mgr.GetOrCreate(r.Context(), sid, req.Tier, orgID)
		if err != nil {
			if strings.Contains(err.Error(), "org mismatch") {
				writeError(w, http.StatusForbidden, "sandbox.org_mismatch", "session belongs to another org")
				return
			}
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

		// 执行后提取 artifact 文件
		var artifacts []ArtifactRef
		if artifactStore != nil {
			artifacts = collectArtifacts(r.Context(), sn, sid, artifactStore, logger)
		}

		writeJSON(w, http.StatusOK, ExecResponse{
			SessionID:  sid,
			Stdout:     result.Stdout,
			Stderr:     result.Stderr,
			ExitCode:   result.ExitCode,
			DurationMs: elapsed,
			Artifacts:  artifacts,
		})
	}
}

// collectArtifacts 从 microVM 拉取产物并上传到对象存储，失败不阻断主流程。
func collectArtifacts(ctx context.Context, sn *session.Session, sessionID string, store *objectstore.Store, logger *logging.JSONLogger) []ArtifactRef {
	fetchResult, err := sn.FetchArtifacts(ctx)
	if err != nil {
		logger.Warn("fetch artifacts failed", logging.LogFields{SessionID: &sessionID}, map[string]any{"error": err.Error()})
		return nil
	}
	if len(fetchResult.Artifacts) == 0 {
		return nil
	}

	refs := make([]ArtifactRef, 0, len(fetchResult.Artifacts))
	for _, entry := range fetchResult.Artifacts {
		data, err := base64.StdEncoding.DecodeString(entry.Data)
		if err != nil {
			logger.Warn("decode artifact base64 failed", logging.LogFields{SessionID: &sessionID}, map[string]any{
				"filename": entry.Filename,
				"error":    err.Error(),
			})
			continue
		}

		key := fmt.Sprintf("%s/%s", sessionID, entry.Filename)
		if err := store.PutWithContentType(ctx, key, data, entry.MimeType); err != nil {
			logger.Warn("upload artifact failed", logging.LogFields{SessionID: &sessionID}, map[string]any{
				"key":   key,
				"error": err.Error(),
			})
			continue
		}

		refs = append(refs, ArtifactRef{
			Key:      key,
			Filename: entry.Filename,
			Size:     entry.Size,
			MimeType: entry.MimeType,
		})
	}
	return refs
}
