//go:build desktop

// Package desktoprun 将 Worker 桌面模式的启动逻辑封装为可复用函数。
// 独立包避免 consumer -> app -> consumer 循环依赖。
package desktoprun

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	api "arkloop/services/api"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	sharedlog "arkloop/services/shared/log"
	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/consumer"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/executor"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// 编译期断言 executor.Registry 满足 pipeline.AgentExecutorBuilder。
var _ pipeline.AgentExecutorBuilder = (*executor.Registry)(nil)

// RunDesktop 启动桌面模式 Worker 消费循环。阻塞直到 ctx 取消或出错。
// 前置条件：worker.InitDesktopInfra() 和 API migration 已完成。
func RunDesktop(ctx context.Context) error {
	// 统一 slog 输出格式（彩色终端或 JSON）
	slog.SetDefault(sharedlog.New(sharedlog.Config{
		Component: "worker",
		Level:     slog.LevelDebug,
		Output:    os.Stdout,
	}))

	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	logger := slog.Default()

	bus, ok := desktop.GetEventBus().(eventbus.EventBus)
	if !ok || bus == nil {
		return fmt.Errorf("event bus not initialized, call InitDesktopInfra first")
	}

	notifier, ok := desktop.GetWorkNotifier().(*consumer.LocalNotifier)
	if !ok || notifier == nil {
		return fmt.Errorf("work notifier not initialized, call InitDesktopInfra first")
	}

	cq, ok := desktop.GetJobEnqueuer().(*queue.ChannelJobQueue)
	if !ok || cq == nil {
		return fmt.Errorf("job queue not initialized, call InitDesktopInfra first")
	}

	writeExecutor := desktop.GetSharedSQLiteWriteExecutor()
	if writeExecutor == nil {
		writeExecutor = sqlitepgx.NewSerialWriteExecutor()
		desktop.SetSharedSQLiteWriteExecutor(writeExecutor)
	}
	sqlitepgx.SetGlobalWriteExecutor(writeExecutor)
	shared := desktop.GetSharedSQLitePool()
	if shared == nil {
		return fmt.Errorf("desktop worker requires shared sqlite pool; start it from the desktop sidecar")
	}
	db := shared.WithWriteExecutor(writeExecutor)

	concurrency := desktopWorkerConcurrency()

	engine, err := app.ComposeDesktopEngine(ctx, db, bus, executor.DefaultExecutorRegistry(), cq)
	if err != nil {
		return fmt.Errorf("compose desktop engine: %w", err)
	}

	lifecycle := newLifecycleManager(db, cq, bus, logger)
	if err := lifecycle.Bootstrap(ctx); err != nil {
		return fmt.Errorf("desktop lifecycle bootstrap: %w", err)
	}
	lifecycle.Start(ctx)

	if err := api.StartDesktopTelegramPollWorker(ctx, db); err != nil {
		return fmt.Errorf("telegram desktop poll: %w", err)
	}

	app.StartDesktopChannelDeliveryDrain(ctx, db)

	handler := &desktopHandler{
		db:     db,
		bus:    bus,
		engine: engine,
		queue:  cq,
		logger: logger,
	}

	loop, err := consumer.NewLoop(
		cq,
		handler,
		consumer.NewLocalRunLocker(),
		consumer.Config{
			Concurrency:        concurrency,
			PollSeconds:        cfg.PollSeconds,
			LeaseSeconds:       cfg.LeaseSeconds,
			HeartbeatSeconds:   cfg.HeartbeatSeconds,
			QueueJobTypes:      cfg.QueueJobTypes,
			MinConcurrency:     2,
			MaxConcurrency:     desktopWorkerConcurrencyHardMax,
			ScaleUpThreshold:   3,
			ScaleDownThreshold: 1,
			ScaleIntervalSecs:  5,
			ScaleCooldownSecs:  30,
		},
		logger,
		notifier,
	)
	if err != nil {
		return err
	}

	logger.Info("desktop worker entering consume mode",
		"concurrency", concurrency,
		"shared_sqlite", true,
		"job_types", cfg.QueueJobTypes,
	)
	return loop.Run(ctx)
}

const desktopWorkerConcurrencyHardMax = 32

// desktopWorkerConcurrency 默认 2，可通过 ARKLOOP_DESKTOP_WORKER_CONCURRENCY 调整（上限 32）。
func desktopWorkerConcurrency() int {
	raw := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_WORKER_CONCURRENCY"))
	if raw == "" {
		return 2
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return 2
	}
	if v > desktopWorkerConcurrencyHardMax {
		return desktopWorkerConcurrencyHardMax
	}
	return v
}

type desktopHandler struct {
	db     data.DesktopDB
	bus    eventbus.EventBus
	engine *app.DesktopEngine
	queue  queue.JobQueue
	logger *slog.Logger
}

func (h *desktopHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	jobType, _ := lease.PayloadJSON["type"].(string)
	traceID, _ := lease.PayloadJSON["trace_id"].(string)
	runIDStr, _ := lease.PayloadJSON["run_id"].(string)
	jobPayload := leasePayloadMap(lease.PayloadJSON)

	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		return fmt.Errorf("parse run_id: %w", err)
	}

	h.logger.Info("desktop handler received job", "job_id", lease.JobID.String(), "trace_id", traceID, "run_id", runIDStr, "job_type", jobType)
	h.publishEvent(ctx, "worker.job.received", map[string]any{
		"job_id":   lease.JobID.String(),
		"job_type": jobType,
		"trace_id": traceID,
		"run_id":   runIDStr,
	})

	runsRepo := data.DesktopRunsRepository{}
	eventsRepo := data.DesktopRunEventsRepository{}

	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	run, err := runsRepo.GetRun(ctx, tx, runID)
	if err != nil {
		return fmt.Errorf("get run: %w", err)
	}
	if run == nil {
		h.logger.Info("run not found, skipped", "job_id", lease.JobID.String(), "trace_id", traceID, "run_id", runIDStr)
		return nil
	}

	terminal, err := eventsRepo.GetLatestEventType(ctx, tx, runID, []string{
		"run.completed", "run.failed", "run.interrupted", "run.cancelled",
	})
	if err != nil {
		return fmt.Errorf("check terminal: %w", err)
	}
	if terminal != "" {
		h.logger.Info("run already terminal, skipped", "job_id", lease.JobID.String(), "trace_id", traceID, "run_id", runIDStr, "terminal_type", terminal)
		return nil
	}

	receivedLogged, err := eventsRepo.GetLatestEventType(ctx, tx, runID, []string{"worker.job.received"})
	if err != nil {
		return fmt.Errorf("check received: %w", err)
	}
	if receivedLogged == "" {
		_, err = eventsRepo.AppendEvent(ctx, tx, runID,
			"worker.job.received",
			map[string]any{
				"trace_id":   traceID,
				"job_id":     lease.JobID.String(),
				"job_type":   jobType,
				"account_id": run.AccountID.String(),
			},
			nil, nil,
		)
		if err != nil {
			return fmt.Errorf("append received event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if err := h.engine.Execute(ctx, *run, traceID, jobPayload); err != nil {
		if handled, resumeErr := tryAutoLoopResumeDesktopRun(ctx, h.db, h.queue, *run, traceID, err); handled {
			if resumeErr != nil {
				slog.ErrorContext(ctx, "desktop auto loop resume failed", "run_id", runIDStr, "err", resumeErr)
			}
			return nil
		}
		slog.ErrorContext(ctx, "desktop engine execute failed", "run_id", runIDStr, "err", err)
		return err
	}

	h.publishEvent(ctx, "worker.job.completed", map[string]any{
		"job_id":   lease.JobID.String(),
		"job_type": jobType,
		"run_id":   runIDStr,
	})

	h.logger.Info("desktop handler completed job", "job_id", lease.JobID.String(), "trace_id", traceID, "run_id", runIDStr)
	return nil
}

func (h *desktopHandler) publishEvent(ctx context.Context, topic string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = h.bus.Publish(ctx, topic, string(raw))
}

func leasePayloadMap(payloadJSON map[string]any) map[string]any {
	if len(payloadJSON) == 0 {
		return nil
	}
	rawPayload, ok := payloadJSON["payload"].(map[string]any)
	if !ok || len(rawPayload) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(rawPayload))
	for key, value := range rawPayload {
		cloned[key] = value
	}
	return cloned
}

const (
	autoLoopResumeMode            = "loop_resume"
	autoLoopResumeSource          = "auto_continue"
	autoLoopResumeErrorClass      = "provider.retryable"
	autoLoopRecoveryErrorClass    = "worker.recovery_resumed"
	autoLoopRecoveryMessage       = "run resumed in a new attempt after desktop recovery"
	autoLoopEnqueueFailureClass   = "worker.recovery_enqueue_failed"
	autoLoopEnqueueFailureMessage = "auto-continue enqueue failed"
)

func tryAutoLoopResumeDesktopRun(
	ctx context.Context,
	db data.DesktopDB,
	jobQueue queue.JobQueue,
	run data.Run,
	traceID string,
	runErr error,
) (bool, error) {
	if db == nil || jobQueue == nil || runErr == nil || !isAutoLoopResumeError(runErr) {
		return false, nil
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	runsRepo := data.DesktopRunsRepository{}
	eventsRepo := data.DesktopRunEventsRepository{}

	currentRun, err := runsRepo.GetRun(ctx, tx, run.ID)
	if err != nil {
		return false, err
	}
	if currentRun == nil {
		return true, nil
	}

	terminalType, err := eventsRepo.GetLatestEventType(ctx, tx, run.ID, []string{
		"run.completed", "run.failed", "run.interrupted", "run.cancelled",
	})
	if err != nil {
		return false, err
	}
	if terminalType != "" {
		return true, nil
	}

	hasRecoverableOutput, err := data.DesktopRunHasRecoverableOutput(ctx, tx, run.ID)
	if err != nil {
		return false, err
	}
	if !hasRecoverableOutput {
		return false, nil
	}

	resumedRun, err := data.DesktopCreateAutoContinueRunInTx(ctx, tx, *currentRun)
	if err != nil {
		if _, markErr := interruptDesktopRunInTx(ctx, tx, run.ID, autoLoopResumeErrorClass, runErr.Error(), map[string]any{
			"recovery_mode":  autoLoopResumeMode,
			"recovery_error": err.Error(),
		}); markErr != nil {
			return false, markErr
		}
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return true, err
	}

	interruptedSeq, err := interruptDesktopRunInTx(ctx, tx, run.ID, autoLoopResumeErrorClass, runErr.Error(), map[string]any{
		"recovery_mode":  autoLoopResumeMode,
		"resumed_run_id": resumedRun.ID.String(),
	})
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}

	if _, err := jobQueue.EnqueueRun(ctx, resumedRun.AccountID, resumedRun.ID, strings.TrimSpace(traceID), queue.RunExecuteJobType, map[string]any{
		"source": autoLoopResumeSource,
	}, nil); err != nil {
		markErr := markAutoResumeEnqueueFailure(ctx, db, run.ID, interruptedSeq, resumedRun.ID, err)
		if markErr != nil {
			return true, fmt.Errorf("%v; enqueue follow-up: %w", markErr, err)
		}
		return true, err
	}
	return true, nil
}

func interruptDesktopRunInTx(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	errorClass string,
	message string,
	details map[string]any,
) (int64, error) {
	payload := map[string]any{
		"error_class": strings.TrimSpace(errorClass),
		"message":     strings.TrimSpace(message),
	}
	if len(details) > 0 {
		payload["details"] = details
	}
	seq, err := (data.DesktopRunEventsRepository{}).AppendEvent(ctx, tx, runID, "run.interrupted", payload, nil, stringPtr(errorClass))
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE runs
		    SET status = 'interrupted',
		        failed_at = datetime('now'),
		        status_updated_at = datetime('now')
		  WHERE id = $1
		    AND status IN ('running', 'cancelling')`,
		runID,
	)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, fmt.Errorf("run not in resumable status: %s", runID)
	}
	return seq, nil
}

func markAutoResumeEnqueueFailure(
	ctx context.Context,
	db data.DesktopDB,
	runID uuid.UUID,
	interruptedSeq int64,
	resumedRunID uuid.UUID,
	enqueueErr error,
) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	if err := updateInterruptedEventDetails(ctx, tx, runID, interruptedSeq, map[string]any{
		"recovery_mode":  autoLoopResumeMode,
		"resumed_run_id": resumedRunID.String(),
		"recovery_error": enqueueErr.Error(),
	}); err != nil {
		return err
	}
	if _, err := (data.DesktopRunEventsRepository{}).AppendEvent(ctx, tx, resumedRunID, "run.failed", map[string]any{
		"error_class": autoLoopEnqueueFailureClass,
		"message":     autoLoopEnqueueFailureMessage,
		"details": map[string]any{
			"reason": enqueueErr.Error(),
		},
	}, nil, stringPtr(autoLoopEnqueueFailureClass)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE runs
		    SET status = 'failed',
		        failed_at = datetime('now'),
		        status_updated_at = datetime('now')
		  WHERE id = $1
		    AND status = 'running'`,
		resumedRunID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func updateInterruptedEventDetails(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	seq int64,
	details map[string]any,
) error {
	var raw string
	if err := tx.QueryRow(ctx,
		`SELECT data_json
		   FROM run_events
		  WHERE run_id = $1
		    AND seq = $2
		  LIMIT 1`,
		runID,
		seq,
	).Scan(&raw); err != nil {
		return err
	}
	payload := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return err
		}
	}
	payload["details"] = details
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE run_events
		    SET data_json = $3
		  WHERE run_id = $1
		    AND seq = $2`,
		runID,
		seq,
		string(encoded),
	); err != nil {
		return err
	}
	return nil
}

func isAutoLoopResumeError(err error) bool {
	if err == nil {
		return false
	}
	switch strings.TrimSpace(err.Error()) {
	case "llm stream idle timeout", "upstream stream ended prematurely without completion":
		return true
	default:
		return false
	}
}
