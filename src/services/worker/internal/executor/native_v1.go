package executor

import (
	"context"
	"fmt"
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

// 工具函数 requiredString / requiredUUID / stringPtr 定义在 helpers.go

type NativeRunEngineV1Handler struct {
	pool   *pgxpool.Pool
	logger *app.JSONLogger
	engine *runengine.EngineV1
	queue  queue.JobQueue
}

func NewNativeRunEngineV1Handler(ctx context.Context, pool *pgxpool.Pool, directPool *pgxpool.Pool, logger *app.JSONLogger, rdb *redis.Client, q queue.JobQueue, cfg app.Config) (*NativeRunEngineV1Handler, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}
	engine, err := app.ComposeNativeEngine(ctx, pool, directPool, rdb, cfg, DefaultExecutorRegistry(), q)
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
	if run.AccountID != payload.AccountID {
		h.logger.Info(
			"job.account_id does not match run.account_id, skipped",
			payload.LogFields(lease),
			map[string]any{"run_account_id": run.AccountID.String()},
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
			"account_id":   payload.AccountID.String(),
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
		"account_id":     run.AccountID.String(),
		"thread_id":  run.ThreadID.String(),
		"status":     status,
		"created_at": createdAt.UTC().Format(time.RFC3339Nano),
	}

	if err := webhook.EnqueueDeliveries(ctx, h.pool, h.queue, run.AccountID, run.ID, payload.TraceID, eventType, runPayload); err != nil {
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
	AccountID   uuid.UUID
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
	accountID, err := requiredUUID(payload, "account_id")
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
		AccountID:   accountID,
		RunID:   runID,
	}, nil
}

func (p workerPayload) LogFields(lease queue.JobLease) app.LogFields {
	fields := app.LogFields{
		JobID: stringPtr(lease.JobID.String()),
	}
	fields.TraceID = stringPtr(p.TraceID)
	fields.AccountID = stringPtr(p.AccountID.String())
	fields.RunID = stringPtr(p.RunID.String())
	return fields
}

