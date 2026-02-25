package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/runlimit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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

type globalRunResponse struct {
	RunID              string   `json:"run_id"`
	OrgID              string   `json:"org_id"`
	ThreadID           string   `json:"thread_id"`
	Status             string   `json:"status"`
	Model              *string  `json:"model,omitempty"`
	SkillID            *string  `json:"skill_id,omitempty"`
	TotalInputTokens   *int64   `json:"total_input_tokens,omitempty"`
	TotalOutputTokens  *int64   `json:"total_output_tokens,omitempty"`
	TotalCostUSD       *float64 `json:"total_cost_usd,omitempty"`
	DurationMs         *int64   `json:"duration_ms,omitempty"`
	CreatedAt          string   `json:"created_at"`
	CompletedAt        *string  `json:"completed_at,omitempty"`
	FailedAt           *string  `json:"failed_at,omitempty"`
}

func createThreadRun(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	apiKeysRepo *data.APIKeysRepository,
	limiter *data.RunLimiter,
	entSvc *entitlement.Service,
	rdb *redis.Client,
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
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}

		var body *createRunRequest
		if err := decodeJSON(r, &body); err != nil {
			if errors.Is(err, io.EOF) {
				body = nil
			} else {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
		}

		startedData := map[string]any{}
		if body != nil && body.RouteID != nil {
			if !routeIDRegex.MatchString(*body.RouteID) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			startedData["route_id"] = *body.RouteID
		}
		if body != nil && body.SkillID != nil {
			if !skillIDRegex.MatchString(*body.SkillID) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			startedData["skill_id"] = strings.TrimSpace(*body.SkillID)
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.create", thread, auditWriter) {
			return
		}

		var acquired bool
		if entSvc != nil && rdb != nil {
			// 从权益系统获取该 org 的并发上限，动态解析覆盖全局配置。
			limitVal, err := entSvc.Resolve(r.Context(), thread.OrgID, "limit.concurrent_runs")
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			key := runlimit.Key(thread.OrgID.String())
			if !runlimit.TryAcquire(r.Context(), rdb, key, limitVal.Int()) {
				WriteError(w, nethttp.StatusTooManyRequests, "runs.limit_exceeded", "concurrent run limit exceeded", traceID, nil)
				return
			}
			acquired = true
			defer func() {
				if acquired {
					runlimit.Release(r.Context(), rdb, key)
				}
			}()
		} else if limiter != nil {
			if !limiter.TryAcquire(r.Context(), thread.OrgID) {
				WriteError(w, nethttp.StatusTooManyRequests, "runs.limit_exceeded", "concurrent run limit exceeded", traceID, nil)
				return
			}
			acquired = true
			defer func() {
				if acquired {
					limiter.Release(r.Context(), thread.OrgID)
				}
			}()
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		runRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		jobRepo, err := data.NewJobRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// Commit 成功，计数器由 Worker 在终态时 DECR
		acquired = false

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
	apiKeysRepo *data.APIKeysRepository,
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
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "runs.list", thread, auditWriter) {
			return
		}

		runs, err := runRepo.ListRunsByThread(r.Context(), actor.OrgID, threadID, limit)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]threadRunResponse, 0, len(runs))
		for _, run := range runs {
			status := run.Status
			if status == "running" {
				terminal, err := runRepo.GetLatestEventType(r.Context(), run.ID, runTerminalEventTypes)
				if err != nil {
					WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
				status = deriveRunStatus(terminal)
			}
			resp = append(resp, threadRunResponse{
				RunID:     run.ID.String(),
				Status:    status,
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
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.get", run, auditWriter) {
			return
		}

		status := run.Status
		if status == "running" {
			terminal, err := runRepo.GetLatestEventType(r.Context(), run.ID, runTerminalEventTypes)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			status = deriveRunStatus(terminal)
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
			Status:          status,
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
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil || pool == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}

		if !authorizeRunOrAudit(w, r, traceID, actor, "runs.cancel", run, auditWriter) {
			return
		}

		tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		defer tx.Rollback(r.Context())

		txRepo, err := data.NewRunEventRepository(tx)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		_, err = txRepo.RequestCancel(r.Context(), run.ID, &actor.UserID, traceID)
		if err != nil {
			var notFound data.RunNotFoundError
			if errors.As(err, &notFound) {
				WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		// 通知 worker 立即中断，失败可忽略（worker 有 DB 兜底检查）
		_, _ = pool.Exec(r.Context(), "SELECT pg_notify($1, '')", "run_cancel_"+run.ID.String())

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
	directPool *pgxpool.Pool,
	sseConfig SSEConfig,
	apiKeysRepo *data.APIKeysRepository,
	rdb *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, runID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
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

		// LISTEN for pg_notify from worker commits
		var notifyCh <-chan struct{}
		if follow && directPool != nil {
			listenConn, err := directPool.Acquire(r.Context())
			if err == nil {
				channel := fmt.Sprintf(`"run_events:%s"`, runID.String())
				if _, err := listenConn.Exec(r.Context(), "LISTEN "+channel); err == nil {
					ch := make(chan struct{}, 1)
					notifyCh = ch
					go func() {
						defer listenConn.Release()
						for {
							if err := listenConn.Conn().PgConn().WaitForNotification(r.Context()); err != nil {
								return
							}
							select {
							case ch <- struct{}{}:
							default:
							}
						}
					}()
				} else {
					listenConn.Release()
				}
			}
		}

		// Redis Pub/Sub 跨实例广播
		var redisCh <-chan struct{}
		if follow && rdb != nil {
			redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", runID.String())
			sub := rdb.Subscribe(r.Context(), redisChannel)
			msgCh := sub.Channel()
			ch := make(chan struct{}, 1)
			redisCh = ch
			go func() {
				defer sub.Close()
				for {
					select {
					case <-r.Context().Done():
						return
					case _, ok := <-msgCh:
						if !ok {
							return
						}
						select {
						case ch <- struct{}{}:
						default:
						}
					}
				}
			}()
		}

		// 合并 pg_notify + Redis Pub/Sub 为单一持久信号 channel，避免循环内重复创建 goroutine。
		var sigCh <-chan struct{}
		if notifyCh != nil && redisCh != nil {
			merged := make(chan struct{}, 1)
			sigCh = merged
			go func() {
				for {
					select {
					case <-r.Context().Done():
						return
					case <-notifyCh:
					case <-redisCh:
					}
					select {
					case merged <- struct{}{}:
					default:
					}
				}
			}()
		} else if notifyCh != nil {
			sigCh = notifyCh
		} else if redisCh != nil {
			sigCh = redisCh
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

			if sigCh != nil {
				select {
				case <-r.Context().Done():
					return
				case <-sigCh:
				case <-time.After(heartbeatDuration):
				}
			} else {
				// fallback: poll at heartbeat interval
				select {
				case <-r.Context().Done():
					return
				case <-time.After(heartbeatDuration):
				}
			}
		}
	}
}

// writeSseEvent writes a single RunEvent to the response stream in SSE format.
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
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
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
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
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
		return 0, fmt.Errorf("must not be negative")
	}
	return v, nil
}

func parseInt64(raw string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &v)
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	return v, nil
}

func runEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
	sseConfig SSEConfig,
	apiKeysRepo *data.APIKeysRepository,
	rdb *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	get := getRun(authService, membershipRepo, runRepo, auditWriter, apiKeysRepo)
	cancel := cancelRun(authService, membershipRepo, runRepo, auditWriter, pool, apiKeysRepo)
	streamEvents := streamRunEvents(authService, membershipRepo, runRepo, auditWriter, directPool, sseConfig, apiKeysRepo, rdb)

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
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
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
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
		"access denied",
		traceID,
		map[string]any{"action": action},
	)
	return false
}

func listGlobalRuns(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	runRepo *data.RunEventRepository,
	apiKeysRepo *data.APIKeysRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		isPlatformAdmin := actor.HasPermission(auth.PermPlatformAdmin)
		if !isPlatformAdmin {
			if !requirePerm(actor, auth.PermDataRunsRead, w, traceID) {
				return
			}
		}

		q := r.URL.Query()
		params := data.ListRunsParams{}

		if rawOrg := q.Get("org_id"); rawOrg != "" {
			parsed, err := uuid.Parse(rawOrg)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid org_id", traceID, nil)
				return
			}
			if !isPlatformAdmin && parsed != actor.OrgID {
				WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "access denied", traceID, nil)
				return
			}
			params.OrgID = &parsed
		} else if !isPlatformAdmin {
			params.OrgID = &actor.OrgID
		}

		if v := q.Get("status"); v != "" {
			v = strings.TrimSpace(v)
			params.Status = &v
		}
		if v := q.Get("since"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid since: must be RFC3339", traceID, nil)
				return
			}
			params.Since = &t
		}
		if v := q.Get("until"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid until: must be RFC3339", traceID, nil)
				return
			}
			params.Until = &t
		}
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 200 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "limit must be 1-200", traceID, nil)
				return
			}
			params.Limit = n
		}
		if v := q.Get("offset"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "offset must be >= 0", traceID, nil)
				return
			}
			params.Offset = n
		}

		runs, total, err := runRepo.ListRuns(r.Context(), params)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]globalRunResponse, 0, len(runs))
		for _, run := range runs {
			item := globalRunResponse{
				RunID:             run.ID.String(),
				OrgID:             run.OrgID.String(),
				ThreadID:          run.ThreadID.String(),
				Status:            run.Status,
				Model:             run.Model,
				SkillID:           run.SkillID,
				TotalInputTokens:  run.TotalInputTokens,
				TotalOutputTokens: run.TotalOutputTokens,
				TotalCostUSD:      run.TotalCostUSD,
				DurationMs:        run.DurationMs,
				CreatedAt:         run.CreatedAt.UTC().Format(time.RFC3339Nano),
			}
			if run.CompletedAt != nil {
				s := run.CompletedAt.UTC().Format(time.RFC3339Nano)
				item.CompletedAt = &s
			}
			if run.FailedAt != nil {
				s := run.FailedAt.UTC().Format(time.RFC3339Nano)
				item.FailedAt = &s
			}
			resp = append(resp, item)
		}

		writeJSON(w, traceID, nethttp.StatusOK, map[string]any{
			"data":  resp,
			"total": total,
		})
	}
}
