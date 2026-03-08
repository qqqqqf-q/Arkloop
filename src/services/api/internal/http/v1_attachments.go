package http

import (
	"context"
	"fmt"
	"io"
	nethttp "net/http"
	"path/filepath"
	"strings"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
)

const messageAttachmentOwnerKind = "message_attachment"
const uploadMultipartBodyLimit = (20 << 20) + (1 << 20)

func uploadThreadAttachment(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	store messageAttachmentStore,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || store == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "attachments.not_configured", "attachment storage not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}
		if !authorizeThreadOrAudit(w, r, traceID, actor, "attachments.create", thread, auditWriter) {
			return
		}

		r.Body = nethttp.MaxBytesReader(w, r.Body, uploadMultipartBodyLimit)
		if err := r.ParseMultipartForm(uploadMultipartBodyLimit); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": "invalid multipart body"})
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": "file is required"})
			return
		}
		defer file.Close()

		filename := sanitizeAttachmentFilename(header.Filename)
		if filename == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": "invalid filename"})
			return
		}

		dataBytes, err := io.ReadAll(io.LimitReader(file, maxImageAttachmentBytes+1))
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		declaredMime := strings.TrimSpace(header.Header.Get("Content-Type"))
		payload, err := buildAttachmentUploadPayload(filename, declaredMime, dataBytes)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": err.Error()})
			return
		}

		threadIDText := thread.ID.String()
		key := fmt.Sprintf("threads/%s/attachments/%s/%s", thread.ID.String(), uuid.NewString(), sanitizeAttachmentKeyName(filename))
		metadata := objectstore.ArtifactMetadata(messageAttachmentOwnerKind, actor.UserID.String(), actor.OrgID.String(), &threadIDText)
		if err := store.PutObject(r.Context(), key, payload.bytes, objectstore.PutOptions{ContentType: payload.mimeType, Metadata: metadata}); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusCreated, messageAttachmentUploadResponse{
			Key:           key,
			Filename:      filename,
			MimeType:      payload.mimeType,
			Size:          int64(len(payload.bytes)),
			Kind:          payload.kind,
			ExtractedText: payload.extractedText,
		})
	}
}

func messageAttachmentsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	threadShareRepo *data.ThreadShareRepository,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	store messageAttachmentStore,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}
		if store == nil || threadRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "attachments.not_configured", "attachment storage not configured", traceID, nil)
			return
		}

		key := strings.TrimPrefix(r.URL.Path, "/v1/attachments/")
		if key == "" || strings.Contains(key, "..") {
			WriteError(w, nethttp.StatusBadRequest, "attachments.invalid_key", "invalid attachment key", traceID, nil)
			return
		}

		info, err := store.Head(r.Context(), key)
		if err != nil {
			if objectstore.IsNotFound(err) {
				WriteError(w, nethttp.StatusNotFound, "attachments.not_found", "attachment not found", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		thread, ok := resolveAttachmentThread(r.Context(), threadRepo, info)
		if !ok || thread == nil {
			WriteError(w, nethttp.StatusForbidden, "attachments.forbidden", "access denied", traceID, nil)
			return
		}

		shareToken := strings.TrimSpace(r.URL.Query().Get("share_token"))
		hasAuthorization := strings.TrimSpace(r.Header.Get("Authorization")) != ""
		if !hasAuthorization && shareToken != "" {
			if !authorizeAttachmentShare(w, r, traceID, threadShareRepo, shareToken, thread) {
				return
			}
		} else {
			if authService == nil {
				writeAuthNotConfigured(w, traceID)
				return
			}
			actor, authenticated := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
			if !authenticated {
				return
			}
			if !requirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
				return
			}
			if !authorizeThreadReadOrAudit(w, r, traceID, actor, "attachments.get", thread, projectRepo, teamRepo, auditWriter) {
				return
			}
		}

		blobData, contentType, err := store.GetWithContentType(r.Context(), key)
		if err != nil {
			if objectstore.IsNotFound(err) {
				WriteError(w, nethttp.StatusNotFound, "attachments.not_found", "attachment not found", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write(blobData)
	}
}

type attachmentUploadPayload struct {
	kind          string
	mimeType      string
	bytes         []byte
	extractedText string
}

func buildAttachmentUploadPayload(filename string, declaredMime string, dataBytes []byte) (attachmentUploadPayload, error) {
	if len(dataBytes) == 0 {
		return attachmentUploadPayload{}, fmt.Errorf("file must not be empty")
	}
	if len(dataBytes) > maxImageAttachmentBytes {
		return attachmentUploadPayload{}, fmt.Errorf("attachment too large")
	}
	mimeType := normalizeUploadedMIME(declaredMime, dataBytes)
	if _, ok := supportedImageMIMEs[mimeType]; ok {
		if len(dataBytes) > maxImageAttachmentBytes {
			return attachmentUploadPayload{}, fmt.Errorf("image attachment too large")
		}
		return attachmentUploadPayload{kind: messagecontent.PartTypeImage, mimeType: mimeType, bytes: dataBytes}, nil
	}
	if !isSupportedTextAttachment(filename, mimeType) {
		return attachmentUploadPayload{}, fmt.Errorf("unsupported attachment type")
	}
	if len(dataBytes) > maxTextAttachmentBytes {
		return attachmentUploadPayload{}, fmt.Errorf("text attachment too large")
	}
	text := strings.ToValidUTF8(string(dataBytes), "")
	text = trimToRunes(text, maxTextAttachmentRunes)
	return attachmentUploadPayload{
		kind:          messagecontent.PartTypeFile,
		mimeType:      normalizeTextMIME(mimeType, filename),
		bytes:         dataBytes,
		extractedText: text,
	}, nil
}

func normalizeUploadedMIME(declared string, dataBytes []byte) string {
	cleaned := strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	if cleaned != "" && cleaned != "application/octet-stream" {
		return cleaned
	}
	return strings.ToLower(strings.TrimSpace(nethttp.DetectContentType(dataBytes)))
}

func normalizeTextMIME(mimeType string, filename string) string {
	if strings.HasPrefix(mimeType, "text/") || mimeType == "application/json" || mimeType == "application/xml" {
		return mimeType
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".csv":
		return "text/csv"
	default:
		return "text/plain"
	}
}

func sanitizeAttachmentFilename(raw string) string {
	base := filepath.Base(strings.TrimSpace(raw))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("._- ()[]", r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.TrimSpace(b.String())
}

func sanitizeAttachmentKeyName(raw string) string {
	name := sanitizeAttachmentFilename(raw)
	if name == "" {
		return "file"
	}
	return strings.ReplaceAll(name, " ", "_")
}

func resolveAttachmentThread(ctx context.Context, threadRepo *data.ThreadRepository, info objectstore.ObjectInfo) (*data.Thread, bool) {
	metadata := info.Metadata
	if strings.TrimSpace(metadata[objectstore.ArtifactMetaOwnerKind]) != messageAttachmentOwnerKind {
		return nil, false
	}
	threadIDText := strings.TrimSpace(metadata[objectstore.ArtifactMetaThreadID])
	if threadIDText == "" {
		return nil, false
	}
	threadID, err := uuid.Parse(threadIDText)
	if err != nil {
		return nil, false
	}
	thread, err := threadRepo.GetByID(ctx, threadID)
	if err != nil || thread == nil {
		return nil, false
	}
	if orgID := strings.TrimSpace(metadata[objectstore.ArtifactMetaOrgID]); orgID != "" && orgID != thread.OrgID.String() {
		return nil, false
	}
	return thread, true
}

func authorizeAttachmentShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	threadShareRepo *data.ThreadShareRepository,
	shareToken string,
	thread *data.Thread,
) bool {
	if threadShareRepo == nil || thread == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return false
	}
	share, err := threadShareRepo.GetByToken(r.Context(), shareToken)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	if share == nil || share.ThreadID != thread.ID {
		WriteError(w, nethttp.StatusForbidden, "attachments.forbidden", "access denied", traceID, nil)
		return false
	}
	if share.AccessType == "password" {
		sessionToken := strings.TrimSpace(r.URL.Query().Get("session_token"))
		if sessionToken == "" || !validateShareSession(sessionToken, share) {
			WriteError(w, nethttp.StatusForbidden, "attachments.forbidden", "access denied", traceID, nil)
			return false
		}
	}
	return true
}
