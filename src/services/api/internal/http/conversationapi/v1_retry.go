package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"errors"
	"io"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func createThreadRunForSource(
	ctx context.Context,
	runRepo *data.RunEventRepository,
	jobRepo *data.JobRepository,
	accountID uuid.UUID,
	threadID uuid.UUID,
	createdByUserID *uuid.UUID,
	traceID string,
	startedData map[string]any,
	jobData map[string]any,
) (data.Run, error) {
	run, _, err := runRepo.CreateRootRunWithClaim(
		ctx,
		accountID,
		threadID,
		createdByUserID,
		"run.started",
		startedData,
	)
	if err != nil {
		return data.Run{}, err
	}

	_, err = jobRepo.EnqueueRun(
		ctx,
		accountID,
		run.ID,
		traceID,
		data.RunExecuteJobType,
		jobData,
		nil,
	)
	if err != nil {
		return data.Run{}, err
	}
	return run, nil
}

func writeThreadRunBusyOrInternal(w nethttp.ResponseWriter, traceID string, err error) {
	if errors.Is(err, data.ErrThreadBusy) {
		httpkit.WriteError(w, nethttp.StatusConflict, "runs.thread_busy", "thread already running", traceID, nil)
		return
	}
	httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
}

type continueThreadRequest struct {
	RunID string `json:"run_id"`
}

func editThreadMessage(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	pool data.TxStarter,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID, messageID uuid.UUID) {
		if r.Method != nethttp.MethodPatch {
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
		if err := httpkit.DecodeJSON(r, &body); err != nil {
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "messages.edit", thread, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		txMessageRepo, err := data.NewMessageRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		existingMessage, err := txMessageRepo.GetByID(r.Context(), thread.AccountID, threadID, messageID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if existingMessage == nil || existingMessage.Role != "user" {
			httpkit.WriteError(w, nethttp.StatusNotFound, "messages.not_found", "message not found or not editable", traceID, nil)
			return
		}

		_, projection, contentJSON, err := normalizeEditedMessagePayload(existingMessage.ContentJSON, body)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": err.Error()})
			return
		}

		_, err = txMessageRepo.UpdateStructuredContent(r.Context(), thread.AccountID, threadID, messageID, projection, contentJSON)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "messages.not_found", "message not found or not editable", traceID, nil)
			return
		}

		if err := txMessageRepo.HideMessagesAfter(r.Context(), thread.AccountID, threadID, messageID); err != nil {
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

		run, _, err := runRepo.CreateRunWithStartedEvent(
			r.Context(),
			thread.AccountID,
			thread.ID,
			&actor.UserID,
			"run.started",
			map[string]any{"source": "edit"},
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		_, err = jobRepo.EnqueueRun(
			r.Context(),
			thread.AccountID,
			run.ID,
			traceID,
			data.RunExecuteJobType,
			map[string]any{"source": "edit"},
			nil,
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

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

func retryThread(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	pool data.TxStarter,
	apiKeysRepo *data.APIKeysRepository,
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
		if threadRepo == nil || messageRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsWrite, w, traceID) {
			return
		}

		var body *createRunRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			if errors.Is(err, io.EOF) {
				body = nil
			} else {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.create", thread, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

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

		latestVisibleMessage, err := txMessageRepo.GetLatestVisibleMessage(r.Context(), thread.AccountID, thread.ID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if latestVisibleMessage == nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "runs.invalid_state", "no message to retry", traceID, nil)
			return
		}
		if latestVisibleMessage.Role == "assistant" {
			if _, err := txMessageRepo.HideLastAssistantMessage(r.Context(), thread.AccountID, thread.ID); err != nil {
				var noMsg data.NoAssistantMessageError
				if errors.As(err, &noMsg) {
					httpkit.WriteError(w, nethttp.StatusBadRequest, "runs.invalid_state", "no assistant message to retry", traceID, nil)
					return
				}
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		} else if latestVisibleMessage.Role != "user" {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "runs.invalid_state", "no user message to retry", traceID, nil)
			return
		}

		startedData := map[string]any{"source": "retry"}
		jobData := map[string]any{"source": "retry"}
		if body != nil && body.Model != nil {
			model := strings.TrimSpace(*body.Model)
			if model != "" {
				startedData["model"] = model
				jobData["model"] = model
			}
		}

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

func continueThread(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
	pool data.TxStarter,
	apiKeysRepo *data.APIKeysRepository,
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
		if threadRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsWrite, w, traceID) {
			return
		}

		var body continueThreadRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		runID, err := uuid.Parse(strings.TrimSpace(body.RunID))
		if err != nil {
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.create", thread, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

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

		parentRun, err := runRepo.GetRunForAccount(r.Context(), thread.AccountID, runID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if parentRun == nil || parentRun.ThreadID != thread.ID {
			httpkit.WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}
		if parentRun.ParentRunID != nil || (parentRun.Status != "cancelled" && parentRun.Status != "interrupted") {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "runs.invalid_state", "run cannot continue", traceID, nil)
			return
		}

		hasOutput, err := runRepo.HasRecoverableAssistantOutput(r.Context(), parentRun.ID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if !hasOutput {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "runs.invalid_state", "run has no output to continue", traceID, nil)
			return
		}

		run, _, err := runRepo.CreateRootRunWithResume(
			r.Context(),
			thread.AccountID,
			thread.ID,
			&actor.UserID,
			"run.started",
			map[string]any{"source": "continue"},
			parentRun.ID,
		)
		if err != nil {
			writeThreadRunBusyOrInternal(w, traceID, err)
			return
		}

		_, err = jobRepo.WithTx(tx).EnqueueRun(
			r.Context(),
			thread.AccountID,
			run.ID,
			traceID,
			data.RunExecuteJobType,
			map[string]any{"source": "continue"},
			nil,
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

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
