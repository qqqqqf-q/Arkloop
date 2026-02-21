package http

import (
	"errors"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func retryThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil || pool == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.create", thread, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		txMessageRepo, err := data.NewMessageRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}

		_, err = txMessageRepo.HideLastAssistantMessage(r.Context(), thread.OrgID, thread.ID)
		if err != nil {
			var noMsg data.NoAssistantMessageError
			if errors.As(err, &noMsg) {
				WriteError(w, nethttp.StatusBadRequest, "runs.invalid_state", "no assistant message to retry", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}

		runRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}
		jobRepo, err := data.NewJobRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}

		run, _, err := runRepo.CreateRunWithStartedEvent(
			r.Context(),
			thread.OrgID,
			thread.ID,
			&actor.UserID,
			"run.started",
			map[string]any{"source": "retry"},
		)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}

		_, err = jobRepo.EnqueueRun(
			r.Context(),
			thread.OrgID,
			run.ID,
			traceID,
			data.RunExecuteJobType,
			map[string]any{"source": "retry"},
			nil,
		)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "internal error", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusCreated, createRunResponse{
			RunID:   run.ID.String(),
			TraceID: traceID,
		})
	}
}
