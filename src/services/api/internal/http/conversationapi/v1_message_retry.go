package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"errors"
	"io"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func retryThreadMessage(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	pool data.TxStarter,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID, messageID uuid.UUID) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil || pool == nil {
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
		if err := httpkit.DecodeJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "messages.retry", thread, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()

		txMessageRepo, err := data.NewMessageRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		runRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		jobRepo, err := data.NewJobRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		txThreadRepo, err := data.NewThreadRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		existingMessage, err := txMessageRepo.GetByID(r.Context(), thread.AccountID, threadID, messageID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if existingMessage == nil || existingMessage.Role != "user" || existingMessage.Hidden {
			httpkit.WriteError(w, nethttp.StatusNotFound, "messages.not_found", "message not found or not editable", traceID, nil)
			return
		}

		if err := txMessageRepo.HideMessagesAfter(r.Context(), thread.AccountID, threadID, messageID); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		startedData := map[string]any{"source": "retry"}
		jobData := map[string]any{"source": "retry"}
		parentRun, parentStartedData, err := resolveEditRunExecutionParent(r.Context(), runRepo, thread.AccountID, thread.ID, messageID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		inheritRunExecutionData(startedData, jobData, parentStartedData, parentRun)
		if err := applyEditRunRequestOverrides(startedData, jobData, body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": err.Error()})
			return
		}

		currentThread, collaborationMode, collaborationModeRevision, err := resolveRunThreadCollaborationMode(r.Context(), txThreadRepo, *thread)
		if err != nil {
			if errors.Is(err, errRunThreadNotFound) {
				httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		thread = &currentThread
		setRunCollaborationMode(startedData, jobData, collaborationMode, collaborationModeRevision)
		setRunLearningMode(startedData, jobData, thread.LearningModeEnabled)

		run, err := createThreadRunForSource(
			r.Context(),
			runRepo,
			jobRepo.WithTx(tx),
			thread.AccountID,
			thread.ID,
			&actor.UserID,
			traceID,
			startedData,
			jobData,
		)
		if err != nil {
			writeThreadRunBusyOrInternal(w, traceID, err)
			return
		}
		if err := inheritRunRefsFromParent(r.Context(), tx, run, parentRun); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		_ = txThreadRepo.Touch(r.Context(), threadID)

		if err := tx.Commit(r.Context()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, createRunResponse{
			RunID:   run.ID.String(),
			TraceID: traceID,
		})
	}
}
