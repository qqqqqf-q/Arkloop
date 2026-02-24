package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/runengine"
	"arkloop/services/worker/internal/webhook"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type NativeRunEngineV1Handler struct {
	pool   *pgxpool.Pool
	logger *app.JSONLogger
	engine *runengine.EngineV1
	queue  queue.JobQueue
}

func NewNativeRunEngineV1Handler(pool *pgxpool.Pool, directPool *pgxpool.Pool, logger *app.JSONLogger, rdb *redis.Client, q queue.JobQueue) (*NativeRunEngineV1Handler, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}
	engine, err := app.ComposeNativeEngine(context.Background(), pool, directPool, rdb)
	if err != nil {
		return nil, err
	}
	return &NativeRunEngineV1Handler{
		pool:   pool,
		logger: logger,
		engine: engine,
		queue:  q,
	}, nil
}

func (h *NativeRunEngineV1Handler) Handle(ctx context.Context, lease queue.JobLease) error {
	payload, err := parseWorkerPayload(lease.PayloadJSON)
	if err != nil {
		return err
	}

	h.logger.Info("worker received job", payload.LogFields(lease), map[string]any{"job_type": payload.JobType})

	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	runsRepo := data.RunsRepository{}
	eventsRepo := data.RunEventsRepository{}

	run, err := runsRepo.GetRun(ctx, tx, payload.RunID)
	if err != nil {
		return err
	}
	if run == nil {
		h.logger.Info("run not found, skipped", payload.LogFields(lease), nil)
		return nil
	}
	if run.OrgID != payload.OrgID {
		h.logger.Info(
			"job.org_id does not match run.org_id, skipped",
			payload.LogFields(lease),
			map[string]any{"run_org_id": run.OrgID.String()},
		)
		return nil
	}

	terminal, err := eventsRepo.GetLatestEventType(ctx, tx, payload.RunID, []string{
		"run.completed",
		"run.failed",
		"run.cancelled",
	})
	if err != nil {
		return err
	}
	if terminal != "" {
		h.logger.Info("run already terminal, skipped", payload.LogFields(lease), map[string]any{"terminal_type": terminal})
		return nil
	}

	_, err = eventsRepo.AppendEvent(
		ctx,
		tx,
		payload.RunID,
		"worker.job.received",
		map[string]any{
			"trace_id": payload.TraceID,
			"job_id":   payload.JobID.String(),
			"job_type": payload.JobType,
			"org_id":   payload.OrgID.String(),
		},
		nil,
		nil,
	)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	err = h.engine.Execute(
		ctx,
		h.pool,
		*run,
		runengine.ExecuteInput{TraceID: payload.TraceID},
	)
	if err != nil {
		return err
	}

	// run 执行完毕后触发 webhook 投递
	// 使用独立 context，避免 job lease ctx 取消导致入队失败
	bgCtx := context.WithoutCancel(ctx)
	if h.queue != nil {
		h.dispatchWebhooks(bgCtx, payload, run)
	}
	return nil
}

// dispatchWebhooks 在 run 终态后为订阅了该事件的端点入队投递 job。
func (h *NativeRunEngineV1Handler) dispatchWebhooks(ctx context.Context, payload workerPayload, run *data.Run) {
	status, createdAt, err := getRunStatus(ctx, h.pool, run.ID)
	if err != nil || status == "" {
		return
	}

	eventType := "run." + status
	runPayload := map[string]any{
		"event":      eventType,
		"run_id":     run.ID.String(),
		"org_id":     run.OrgID.String(),
		"thread_id":  run.ThreadID.String(),
		"status":     status,
		"created_at": createdAt.UTC().Format(time.RFC3339Nano),
	}

	if err := webhook.EnqueueDeliveries(ctx, h.pool, h.queue, run.OrgID, run.ID, payload.TraceID, eventType, runPayload); err != nil {
		h.logger.Error("enqueue webhook deliveries failed", payload.LogFields(queue.JobLease{}), map[string]any{"error": err.Error()})
	}
}

// getRunStatus 查询 run 的当前终态状态和创建时间。
func getRunStatus(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID) (string, time.Time, error) {
	var status string
	var createdAt time.Time
	err := pool.QueryRow(ctx,
		`SELECT status, created_at FROM runs WHERE id = $1`,
		runID,
	).Scan(&status, &createdAt)
	if err != nil {
		return "", time.Time{}, err
	}
	return status, createdAt, nil
}

type workerPayload struct {
	JobID   uuid.UUID
	JobType string
	TraceID string
	OrgID   uuid.UUID
	RunID   uuid.UUID
}

func parseWorkerPayload(payload map[string]any) (workerPayload, error) {
	jobID, err := requiredUUID(payload, "job_id")
	if err != nil {
		return workerPayload{}, err
	}
	jobType, err := requiredString(payload, "type")
	if err != nil {
		return workerPayload{}, err
	}
	traceID, err := requiredString(payload, "trace_id")
	if err != nil {
		return workerPayload{}, err
	}
	orgID, err := requiredUUID(payload, "org_id")
	if err != nil {
		return workerPayload{}, err
	}
	runID, err := requiredUUID(payload, "run_id")
	if err != nil {
		return workerPayload{}, err
	}
	return workerPayload{
		JobID:   jobID,
		JobType: jobType,
		TraceID: traceID,
		OrgID:   orgID,
		RunID:   runID,
	}, nil
}

func (p workerPayload) LogFields(lease queue.JobLease) app.LogFields {
	fields := app.LogFields{
		JobID: stringPtr(lease.JobID.String()),
	}
	fields.TraceID = stringPtr(p.TraceID)
	fields.OrgID = stringPtr(p.OrgID.String())
	fields.RunID = stringPtr(p.RunID.String())
	return fields
}

func requiredString(values map[string]any, key string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return cleaned, nil
}

func requiredUUID(values map[string]any, key string) (uuid.UUID, error) {
	text, err := requiredString(values, key)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s is not a valid UUID", key)
	}
	return id, nil
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
