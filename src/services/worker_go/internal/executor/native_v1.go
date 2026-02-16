package executor

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/worker_go/internal/app"
	"arkloop/services/worker_go/internal/data"
	"arkloop/services/worker_go/internal/queue"
	"arkloop/services/worker_go/internal/runengine"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NativeRunEngineV1Handler struct {
	pool   *pgxpool.Pool
	logger *app.JSONLogger
	engine *runengine.EngineV1
}

func NewNativeRunEngineV1Handler(pool *pgxpool.Pool, logger *app.JSONLogger) (*NativeRunEngineV1Handler, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool 不能为空")
	}
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}
	engine, err := runengine.NewEngineV1()
	if err != nil {
		return nil, err
	}
	return &NativeRunEngineV1Handler{
		pool:   pool,
		logger: logger,
		engine: engine,
	}, nil
}

func (h *NativeRunEngineV1Handler) Handle(ctx context.Context, lease queue.JobLease) error {
	payload, err := parseWorkerPayload(lease.PayloadJSON)
	if err != nil {
		return err
	}

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
		h.logger.Info("run 不存在，已跳过", payload.LogFields(lease), nil)
		return nil
	}
	if run.OrgID != payload.OrgID {
		h.logger.Info(
			"job.org_id 与 run.org_id 不一致，已跳过",
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
		h.logger.Info("run 已终态，已跳过", payload.LogFields(lease), map[string]any{"terminal_type": terminal})
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
	return nil
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
		return "", fmt.Errorf("缺少 %s", key)
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s 必须为字符串", key)
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return "", fmt.Errorf("%s 不能为空", key)
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
		return uuid.Nil, fmt.Errorf("%s 不是合法 UUID", key)
	}
	return id, nil
}
