package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api_go/internal/audit"
	"arkloop/services/api_go/internal/auth"
	"arkloop/services/api_go/internal/data"
	"arkloop/services/api_go/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	routeIDRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	skillIDRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}(?:@[A-Za-z0-9][A-Za-z0-9._:-]{0,63})?$`)
)

var runTerminalEventTypes = []string{"run.completed", "run.failed", "run.cancelled"}

type createRunRequest struct {
	RouteID *string `json:"route_id"`
	SkillID *string `json:"skill_id"`
}

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
	RunID           string  `json:"run_id"`
	OrgID           string  `json:"org_id"`
	ThreadID        string  `json:"thread_id"`
	CreatedByUserID *string `json:"created_by_user_id"`
	Status          string  `json:"status"`
	CreatedAt       string  `json:"created_at"`
	TraceID         string  `json:"trace_id"`
}

type cancelRunResponse struct {
	OK bool `json:"ok"`
}

func createThreadRun(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
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
		if threadRepo == nil || pool == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		var body *createRunRequest
		if err := decodeJSON(r, &body); err != nil {
			if errors.Is(err, io.EOF) {
				body = nil
			} else {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
				return
			}
		}

		startedData := map[string]any{}
		if body != nil && body.RouteID != nil {
			if !routeIDRegex.MatchString(*body.RouteID) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
				return
			}
			startedData["route_id"] = *body.RouteID
		}
		if body != nil && body.SkillID != nil {
			if !skillIDRegex.MatchString(*body.SkillID) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
				return
			}
			startedData["skill_id"] = strings.TrimSpace(*body.SkillID)
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if thread == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "Thread 不存在", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.create", thread, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		runRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		jobRepo, err := data.NewJobRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		run, _, err := runRepo.CreateRunWithStartedEvent(
			r.Context(),
			thread.OrgID,
			thread.ID,
			&actor.UserID,
			"run.started",
			startedData,
		)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		_, err = jobRepo.EnqueueRun(
			r.Context(),
			thread.OrgID,
			run.ID,
			traceID,
			data.RunExecuteJobType,
			map[string]any{"source": "api"},
			nil,
		)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusCreated, createRunResponse{
			RunID:   run.ID.String(),
			TraceID: traceID,
		})
	}
}

func listThreadRuns(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || runRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if thread == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "Thread 不存在", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.list", thread, auditWriter) {
			return
		}

		runs, err := runRepo.ListRunsByThread(r.Context(), actor.OrgID, threadID, limit)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		resp := make([]threadRunResponse, 0, len(runs))
		for _, run := range runs {
			terminal, err := runRepo.GetLatestEventType(r.Context(), run.ID, runTerminalEventTypes)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
				return
			}
			resp = append(resp, threadRunResponse{
				RunID:     run.ID.String(),
				Status:    deriveRunStatus(terminal),
				CreatedAt: run.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
		}

		writeJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func getRun(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "Run 不存在", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.get", run, auditWriter) {
			return
		}

		terminal, err := runRepo.GetLatestEventType(r.Context(), run.ID, runTerminalEventTypes)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		var createdByUserID *string
		if run.CreatedByUserID != nil {
			value := run.CreatedByUserID.String()
			createdByUserID = &value
		}

		writeJSON(w, traceID, nethttp.StatusOK, runResponse{
			RunID:           run.ID.String(),
			OrgID:           run.OrgID.String(),
			ThreadID:        run.ThreadID.String(),
			CreatedByUserID: createdByUserID,
			Status:          deriveRunStatus(terminal),
			CreatedAt:       run.CreatedAt.UTC().Format(time.RFC3339Nano),
			TraceID:         traceID,
		})
	}
}

func cancelRun(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil || pool == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "Run 不存在", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.cancel", run, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		txRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		_, err = txRepo.RequestCancel(r.Context(), run.ID, &actor.UserID, traceID)
		if err != nil {
			var notFound data.RunNotFoundError
			if errors.As(err, &notFound) {
				WriteError(w, nethttp.StatusNotFound, "runs.not_found", "Run 不存在", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteRunCancelRequested(r.Context(), traceID, actor.OrgID, actor.UserID, run.ID)
		}

		writeJSON(w, traceID, nethttp.StatusOK, cancelRunResponse{OK: true})
	}
}

func streamRunEvents(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	sseConfig SSEConfig,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "Run 不存在", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.events", run, auditWriter) {
			return
		}

		afterSeq, follow, ok := parseSSEQueryParams(w, traceID, r)
		if !ok {
			return
		}

		batchLimit := sseConfig.BatchLimit
		if batchLimit <= 0 {
			batchLimit = 500
		}
		pollDuration := time.Duration(float64(time.Second) * sseConfig.PollSeconds)
		heartbeatDuration := time.Duration(float64(time.Second) * sseConfig.HeartbeatSeconds)

		flusher, canFlush := w.(nethttp.Flusher)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(nethttp.StatusOK)

		if follow {
			_, _ = fmt.Fprint(w, ": ping\n\n")
			if canFlush {
				flusher.Flush()
			}
		}

		cursor := afterSeq
		lastSend := time.Now()

		for {
			select {
			case <-r.Context().Done():
				return
			default:
			}

			events, err := runRepo.ListEvents(r.Context(), runID, cursor, batchLimit)
			if err != nil {
				return
			}

			if len(events) > 0 {
				for _, item := range events {
					cursor = item.Seq
					if err := writeSseEvent(w, item); err != nil {
						return
					}
				}
				lastSend = time.Now()
				if canFlush {
					flusher.Flush()
				}
				continue
			}

			if !follow {
				return
			}

			now := time.Now()
			if heartbeatDuration > 0 && now.Sub(lastSend) >= heartbeatDuration {
				_, _ = fmt.Fprint(w, ": ping\n\n")
				if canFlush {
					flusher.Flush()
				}
				lastSend = now
			}

			select {
			case <-r.Context().Done():
				return
			case <-time.After(pollDuration):
			}
		}
	}
}

// writeSseEvent 将单条 RunEvent 按 SSE 格式写入响应流。
func writeSseEvent(w nethttp.ResponseWriter, item data.RunEvent) error {
	ts := item.TS.UTC()
	millis := ts.Format("2006-01-02T15:04:05.999Z07:00")

	payload := map[string]any{
		"event_id": item.EventID.String(),
		"run_id":   item.RunID.String(),
		"seq":      item.Seq,
		"ts":       millis,
		"type":     item.Type,
		"data":     item.DataJSON,
	}
	if payload["data"] == nil {
		payload["data"] = map[string]any{}
	}

	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", item.Seq, item.Type, dataBytes)
	return err
}

func parseSSEQueryParams(
	w nethttp.ResponseWriter,
	traceID string,
	r *nethttp.Request,
) (afterSeq int64, follow bool, ok bool) {
	follow = true
	afterSeq = 0

	if raw := strings.TrimSpace(r.URL.Query().Get("after_seq")); raw != "" {
		parsed, err := parseInt64NonNegative(raw)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return 0, false, false
		}
		afterSeq = parsed
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("follow")); raw != "" {
		switch raw {
		case "true", "1":
			follow = true
		case "false", "0":
			follow = false
		default:
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return 0, false, false
		}
	}

	return afterSeq, follow, true
}

func parseInt64NonNegative(raw string) (int64, error) {
	v, err := parseInt64(raw)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("不能为负数")
	}
	return v, nil
}

func parseInt64(raw string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &v)
	if err != nil {
		return 0, fmt.Errorf("必须为整数")
	}
	return v, nil
}

func runEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	sseConfig SSEConfig,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	get := getRun(authService, membershipRepo, runRepo, auditWriter)
	cancel := cancelRun(authService, membershipRepo, runRepo, auditWriter, pool)
	streamEvents := streamRunEvents(authService, membershipRepo, runRepo, auditWriter, sseConfig)

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		parts := strings.SplitN(tail, "/", 2)
		idPart, actionPart, hasAction := strings.Cut(parts[0], ":")

		runID, err := uuid.Parse(idPart)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}

		if hasAction {
			if actionPart != "cancel" {
				writeNotFound(w, r)
				return
			}
			if r.Method != nethttp.MethodPost {
				writeMethodNotAllowed(w, r)
				return
			}
			cancel(w, r, runID)
			return
		}

		if len(parts) == 1 {
			if r.Method != nethttp.MethodGet {
				writeMethodNotAllowed(w, r)
				return
			}
			get(w, r, runID)
			return
		}

		if parts[1] == "events" {
			if r.Method != nethttp.MethodGet {
				writeMethodNotAllowed(w, r)
				return
			}
			streamEvents(w, r, runID)
			return
		}

		writeNotFound(w, r)
	}
}

func deriveRunStatus(terminalEventType string) string {
	switch terminalEventType {
	case "run.completed":
		return "completed"
	case "run.failed":
		return "failed"
	case "run.cancelled":
		return "cancelled"
	default:
		return "running"
	}
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
		WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
		return false
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

	WriteError(
		w,
		nethttp.StatusForbidden,
		"policy.denied",
		"无权限",
		traceID,
		map[string]any{"action": action},
	)
	return false
}
