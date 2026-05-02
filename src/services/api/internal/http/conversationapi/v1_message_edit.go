package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"errors"
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

func inheritRunRefsFromParent(ctx context.Context, tx pgx.Tx, run data.Run, parentRun *data.Run) error {
	if parentRun == nil || (parentRun.ProfileRef == nil && parentRun.WorkspaceRef == nil) {
		return nil
	}
	_, err := tx.Exec(
		ctx,
		`UPDATE runs
		    SET profile_ref = $2,
		        workspace_ref = $3
		  WHERE id = $1`,
		run.ID,
		parentRun.ProfileRef,
		parentRun.WorkspaceRef,
	)
	return err
}

func inheritRunExecutionData(startedData map[string]any, jobData map[string]any, parentStartedData map[string]any, parentRun *data.Run) {
	copyString := func(key string) {
		if parentStartedData == nil {
			return
		}
		value, ok := parentStartedData[key].(string)
		if !ok {
			return
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		startedData[key] = value
		jobData[key] = value
	}

	for _, key := range []string{
		"route_id",
		"persona_id",
		"role",
		"output_route_id",
		"output_model_key",
		"model",
		"work_dir",
		"reasoning_mode",
	} {
		copyString(key)
	}

	if parentStartedData != nil {
		if value, ok := parentStartedData["timeout_seconds"]; ok {
			switch n := value.(type) {
			case int:
				if n > 0 {
					startedData["timeout_seconds"] = n
					jobData["timeout_seconds"] = n
				}
			case float64:
				if int(n) > 0 {
					startedData["timeout_seconds"] = int(n)
					jobData["timeout_seconds"] = int(n)
				}
			}
		}
	}

	if _, ok := startedData["model"]; !ok && parentRun != nil && parentRun.Model != nil {
		model := strings.TrimSpace(*parentRun.Model)
		if model != "" {
			startedData["model"] = model
			jobData["model"] = model
		}
	}
	if _, ok := startedData["persona_id"]; !ok && parentRun != nil && parentRun.PersonaID != nil {
		personaID := strings.TrimSpace(*parentRun.PersonaID)
		if personaID != "" {
			startedData["persona_id"] = personaID
			jobData["persona_id"] = personaID
		}
	}
}

func hasInheritedRunExecutionData(startedData map[string]any) bool {
	if startedData == nil {
		return false
	}
	for _, key := range []string{
		"route_id",
		"persona_id",
		"role",
		"output_route_id",
		"output_model_key",
		"model",
		"work_dir",
		"reasoning_mode",
		"timeout_seconds",
	} {
		if _, ok := startedData[key]; ok {
			return true
		}
	}
	return false
}

func applyEditRunRequestOverrides(startedData map[string]any, jobData map[string]any, body createMessageRequest) error {
	if body.RouteID != nil {
		routeID := strings.TrimSpace(*body.RouteID)
		if !routeIDRegex.MatchString(routeID) {
			return errors.New("route_id invalid")
		}
		startedData["route_id"] = routeID
		jobData["route_id"] = routeID
	}
	if body.PersonaID != nil {
		personaID := strings.TrimSpace(*body.PersonaID)
		if !personaIDRegex.MatchString(personaID) {
			return errors.New("persona_id invalid")
		}
		startedData["persona_id"] = personaID
		jobData["persona_id"] = personaID
	}
	if body.Model != nil {
		model := strings.TrimSpace(*body.Model)
		if model != "" {
			startedData["model"] = model
			jobData["model"] = model
		}
	}
	if body.WorkDir != nil {
		workDir := strings.TrimSpace(*body.WorkDir)
		if workDir != "" {
			startedData["work_dir"] = workDir
			jobData["work_dir"] = workDir
		}
	}
	if body.ReasoningMode != nil {
		reasoningMode := strings.TrimSpace(*body.ReasoningMode)
		if reasoningMode != "" {
			startedData["reasoning_mode"] = reasoningMode
			jobData["reasoning_mode"] = reasoningMode
		}
	}
	return nil
}

func resolveEditRunExecutionParent(ctx context.Context, runRepo *data.RunEventRepository, accountID uuid.UUID, threadID uuid.UUID, messageID uuid.UUID) (*data.Run, map[string]any, error) {
	runs, err := runRepo.ListRunsByThread(ctx, accountID, threadID, 200)
	if err != nil {
		return nil, nil, err
	}
	messageIDText := messageID.String()
	for i := range runs {
		run := runs[i]
		if run.ParentRunID != nil || run.DeletedAt != nil {
			continue
		}
		startedData, err := runRepo.FirstRunStartedData(ctx, run.ID)
		if err != nil {
			return nil, nil, err
		}
		if tailID, _ := startedData["thread_tail_message_id"].(string); strings.TrimSpace(tailID) != messageIDText {
			continue
		}
		candidateStarted := map[string]any{"source": "edit"}
		candidateJob := map[string]any{"source": "edit"}
		inheritRunExecutionData(candidateStarted, candidateJob, startedData, &run)
		if !hasInheritedRunExecutionData(candidateStarted) {
			continue
		}
		runCopy := run
		return &runCopy, startedData, nil
	}

	parentRun, err := runRepo.GetLatestRootRunForThread(ctx, threadID)
	if err != nil || parentRun == nil {
		return parentRun, nil, err
	}
	startedData, err := runRepo.FirstRunStartedData(ctx, parentRun.ID)
	if err != nil {
		return nil, nil, err
	}
	return parentRun, startedData, nil
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
		defer func() { _ = tx.Rollback(r.Context()) }()

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
		txThreadRepo, err := data.NewThreadRepository(tx)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		startedData := map[string]any{"source": "edit"}
		jobData := map[string]any{"source": "edit"}
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
