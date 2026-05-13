package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
)

func createThreadMessage(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
	store messageAttachmentStore,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
			return
		}

		var body createMessageRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		_, projection, contentJSON, err := normalizeCreateMessagePayload(body)
		if err != nil {
			slog.Warn("createThreadMessage: normalize failed", "error", err)
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": err.Error()})
			return
		}
		clientMessageID := ""
		if body.ClientMessageID != nil {
			parsed, err := uuid.Parse(strings.TrimSpace(*body.ClientMessageID))
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": "client_message_id must be a valid UUID"})
				return
			}
			clientMessageID = parsed.String()
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			slog.Error("createThreadMessage: GetByID failed", "thread_id", threadID, "error", err)
			writeInternalError(w, traceID, err)
			return
		}
		if thread == nil {
			slog.Warn("createThreadMessage: thread not found", "thread_id", threadID)
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		authorized := authorizeThreadOrAudit(w, r, traceID, actor, "messages.create", thread, auditWriter)
		if !authorized {
			return
		}

		if clientMessageID != "" {
			existing, err := messageRepo.FindByClientMessageID(r.Context(), thread.AccountID, threadID, actor.UserID, clientMessageID)
			if err != nil {
				slog.Error("createThreadMessage: FindByClientMessageID failed", "thread_id", threadID, "error", err)
				writeInternalError(w, traceID, err)
				return
			}
			if existing != nil {
				httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toMessageResponse(*existing))
				return
			}
		}

		contentJSON, err = migrateStagingAttachments(r.Context(), store, contentJSON, thread.AccountID, threadID)
		if err != nil {
			slog.Error("createThreadMessage: staging migration failed", "thread_id", threadID, "error", err)
			writeInternalError(w, traceID, err)
			return
		}

		var metadataJSON json.RawMessage
		if clientMessageID != "" {
			metadataJSON, err = json.Marshal(map[string]string{"client_message_id": clientMessageID})
			if err != nil {
				writeInternalError(w, traceID, err)
				return
			}
		}

		// Use thread.AccountID so messages stay on the thread's account even if token claims drift.
		message, err := messageRepo.CreateStructuredWithMetadata(r.Context(), thread.AccountID, threadID, "user", projection, contentJSON, metadataJSON, &actor.UserID)
		if err != nil {
			if clientMessageID != "" {
				existing, findErr := messageRepo.FindByClientMessageID(r.Context(), thread.AccountID, threadID, actor.UserID, clientMessageID)
				if findErr == nil && existing != nil {
					httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toMessageResponse(*existing))
					return
				}
				if findErr != nil {
					slog.Warn("createThreadMessage: post-create FindByClientMessageID failed", "thread_id", threadID, "error", findErr)
				}
			}
			slog.Error("createThreadMessage: CreateStructured failed", "thread_id", threadID, "error", err)
			var threadNotFound data.ThreadNotFoundError
			if errors.As(err, &threadNotFound) {
				httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
				return
			}
			writeInternalError(w, traceID, err)
			return
		}

		if err := threadRepo.Touch(r.Context(), threadID); err != nil {
			slog.Warn("createThreadMessage: Touch failed", "thread_id", threadID, "error", err)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toMessageResponse(message))
	}
}

func listThreadMessages(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
			return
		}

		limit, ok := parseMessageLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			writeInternalError(w, traceID, err)
			return
		}
		if thread == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "messages.list", thread, auditWriter) {
			return
		}

		messages, err := messageRepo.ListByThread(r.Context(), actor.AccountID, threadID, limit)
		if err != nil {
			writeInternalError(w, traceID, err)
			return
		}

		resp := make([]messageResponse, 0, len(messages))
		for _, item := range messages {
			resp = append(resp, toMessageResponse(item))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func parseMessageLimit(w nethttp.ResponseWriter, traceID string, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 200, true
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 1 || parsed > 500 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return 0, false
	}
	return parsed, true
}

func toMessageResponse(message data.Message) messageResponse {
	var createdByUserID *string
	if message.CreatedByUserID != nil {
		value := message.CreatedByUserID.String()
		createdByUserID = &value
	}
	var runID *string
	var clientMessageID *string
	if len(message.MetadataJSON) > 0 {
		var metadata struct {
			RunID           string `json:"run_id"`
			ClientMessageID string `json:"client_message_id"`
		}
		if err := json.Unmarshal(message.MetadataJSON, &metadata); err == nil {
			metadata.RunID = strings.TrimSpace(metadata.RunID)
			if metadata.RunID != "" {
				runID = &metadata.RunID
			}
			metadata.ClientMessageID = strings.TrimSpace(metadata.ClientMessageID)
			if metadata.ClientMessageID != "" {
				clientMessageID = &metadata.ClientMessageID
			}
		}
	}

	return messageResponse{
		ID:              message.ID.String(),
		AccountID:       message.AccountID.String(),
		ThreadID:        message.ThreadID.String(),
		CreatedByUserID: createdByUserID,
		RunID:           runID,
		Role:            message.Role,
		Content:         message.Content,
		ContentJSON:     message.ContentJSON,
		CreatedAt:       message.CreatedAt.UTC().Format(time.RFC3339Nano),
		ClientMessageID: clientMessageID,
	}
}

const stagingKeyPrefix = "staging/"

// migrateStagingAttachments 将 content_json 中 staging/ 前缀的附件 key
// 迁移到 attachments/ 前缀下，同时在对象存储中执行 copy + delete。
func migrateStagingAttachments(ctx context.Context, store messageAttachmentStore, contentJSON json.RawMessage, accountID, threadID uuid.UUID) (json.RawMessage, error) {
	if len(contentJSON) == 0 || store == nil {
		return contentJSON, nil
	}

	parsed, err := messagecontent.Parse(contentJSON)
	if err != nil {
		return contentJSON, nil
	}

	modified := false
	for i, part := range parsed.Parts {
		if part.Attachment == nil {
			continue
		}
		if !strings.HasPrefix(part.Attachment.Key, stagingKeyPrefix) {
			continue
		}

		newKey := fmt.Sprintf("attachments/%s/%s", accountID.String(), strings.TrimPrefix(part.Attachment.Key, stagingKeyPrefix+accountID.String()+"/"))

		dataBytes, contentType, err := store.GetWithContentType(ctx, part.Attachment.Key)
		if err != nil {
			return nil, fmt.Errorf("read staging object %q: %w", part.Attachment.Key, err)
		}

		threadIDText := threadID.String()
		metadata := objectstore.ArtifactMetadata(MessageAttachmentOwnerKind, "", accountID.String(), &threadIDText)
		if err := store.PutObject(ctx, newKey, dataBytes, objectstore.PutOptions{ContentType: contentType, Metadata: metadata}); err != nil {
			return nil, fmt.Errorf("write attachment object %q: %w", newKey, err)
		}

		_ = store.Delete(ctx, part.Attachment.Key)

		parsed.Parts[i].Attachment.Key = newKey
		modified = true
	}

	if !modified {
		return contentJSON, nil
	}

	return json.Marshal(parsed)
}
