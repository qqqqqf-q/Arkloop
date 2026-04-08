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
	"path/filepath"
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

	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}

	sqlitePath := filepath.Join(dataDir, "data.db")
	writeExecutor := desktop.GetSharedSQLiteWriteExecutor()
	if writeExecutor == nil {
		writeExecutor = sqlitepgx.NewSerialWriteExecutor()
		desktop.SetSharedSQLiteWriteExecutor(writeExecutor)
	}
	sqlitepgx.SetGlobalWriteExecutor(writeExecutor)
	var db *sqlitepgx.Pool
	ownsDB := false
	if shared := desktop.GetSharedSQLitePool(); shared != nil {
		db = shared.WithWriteExecutor(writeExecutor)
	} else {
		opened, openErr := sqlitepgx.Open(sqlitePath)
		if openErr != nil {
			return fmt.Errorf("open sqlite: %w", openErr)
		}
		db = opened.WithWriteExecutor(writeExecutor)
		ownsDB = true
	}
	if ownsDB {
		defer db.Close()
	}

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

	handler := &desktopHandler{
		db:     db,
		bus:    bus,
		engine: engine,
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
		"shared_sqlite", !ownsDB,
		"job_types", cfg.QueueJobTypes,
		"sqlite_path", sqlitePath,
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
	defer tx.Rollback(ctx)

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
