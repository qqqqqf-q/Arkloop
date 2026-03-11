package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
)

const (
	shareSessionDuration = 24 * time.Hour
	shareSessionSecret   = "arkloop-share-session"
)

type createRunResponse struct {
	RunID   string `json:"run_id"`
	TraceID string `json:"trace_id"`
}

type threadRunResponse struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type runResponse struct {
	RunID           string   `json:"run_id"`
	OrgID           string   `json:"org_id"`
	ThreadID        string   `json:"thread_id"`
	CreatedByUserID *string  `json:"created_by_user_id"`
	ParentRunID     *string  `json:"parent_run_id,omitempty"`
	ChildRunIDs     []string `json:"child_run_ids,omitempty"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	TraceID         string   `json:"trace_id"`
}

type cancelRunResponse struct {
	OK bool `json:"ok"`
}

type submitInputResponse struct {
	OK bool `json:"ok"`
}

type globalRunResponse struct {
	RunID             string   `json:"run_id"`
	OrgID             string   `json:"org_id"`
	ThreadID          string   `json:"thread_id"`
	Status            string   `json:"status"`
	Model             *string  `json:"model,omitempty"`
	PersonaID         *string  `json:"persona_id,omitempty"`
	ParentRunID       *string  `json:"parent_run_id,omitempty"`
	TotalInputTokens  *int64   `json:"total_input_tokens,omitempty"`
	TotalOutputTokens *int64   `json:"total_output_tokens,omitempty"`
	TotalCostUSD      *float64 `json:"total_cost_usd,omitempty"`
	DurationMs        *int64   `json:"duration_ms,omitempty"`
	CacheHitRate      *float64 `json:"cache_hit_rate,omitempty"`
	CreditsUsed       *int64   `json:"credits_used,omitempty"`
	CreatedAt         string   `json:"created_at"`
	CompletedAt       *string  `json:"completed_at,omitempty"`
	FailedAt          *string  `json:"failed_at,omitempty"`
	CreatedByUserID   *string  `json:"created_by_user_id,omitempty"`
	CreatedByUserName *string  `json:"created_by_user_name,omitempty"`
	CreatedByEmail    *string  `json:"created_by_email,omitempty"`
}

type threadResponse struct {
	ID              string  `json:"id"`
	OrgID           string  `json:"org_id"`
	CreatedByUserID *string `json:"created_by_user_id"`
	Mode            string  `json:"mode"`
	Title           *string `json:"title"`
	ProjectID       *string `json:"project_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
	ActiveRunID     *string `json:"active_run_id"`
	IsPrivate       bool    `json:"is_private"`
	ParentThreadID  *string `json:"parent_thread_id,omitempty"`
}

type forkThreadResponse struct {
	threadResponse
	IDMapping []idMappingPair `json:"id_mapping,omitempty"`
}

type idMappingPair struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
}

type messageResponse struct {
	ID              string          `json:"id"`
	OrgID           string          `json:"org_id"`
	ThreadID        string          `json:"thread_id"`
	CreatedByUserID *string         `json:"created_by_user_id"`
	RunID           *string         `json:"run_id,omitempty"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	ContentJSON     json.RawMessage `json:"content_json,omitempty"`
	CreatedAt       string          `json:"created_at"`
}

type messageAttachmentUploadResponse struct {
	Key           string `json:"key"`
	Filename      string `json:"filename"`
	MimeType      string `json:"mime_type"`
	Size          int64  `json:"size"`
	Kind          string `json:"kind"`
	ExtractedText string `json:"extracted_text,omitempty"`
}

type reportResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

func generateShareSession(share *data.ThreadShare) string {
	expiry := time.Now().Add(shareSessionDuration).Unix()
	payload := fmt.Sprintf("%s:%d", share.Token, expiry)
	key := []byte(shareSessionSecret + share.ID.String())
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s:%d", sig, expiry)
}

func authorizeRunOrAudit(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *actor,
	action string,
	run *data.Run,
	auditWriter *audit.Writer,
) bool {
	if actor == nil || run == nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	if actor.HasPermission(auth.PermPlatformAdmin) {
		return true
	}

	denyReason := "owner_mismatch"
	if actor.OrgID != run.OrgID {
		denyReason = "org_mismatch"
	} else if run.CreatedByUserID == nil {
		denyReason = "no_owner"
	} else if *run.CreatedByUserID == actor.UserID {
		return true
	}

	if auditWriter != nil {
		auditWriter.WriteAccessDenied(
			r.Context(),
			traceID,
			actor.OrgID,
			actor.UserID,
			action,
			"run",
			run.ID.String(),
			run.OrgID,
			run.CreatedByUserID,
			denyReason,
		)
	}

	WriteError(w, nethttp.StatusForbidden, "policy.denied", "access denied", traceID, map[string]any{"action": action})
	return false
}
