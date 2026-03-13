//go:build desktop

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/consumer"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/executor"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func run() error {
	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	logger := app.NewJSONLogger("worker_go", os.Stdout)

	bus := eventbus.NewLocalEventBus()
	defer bus.Close()

	localNotifier := consumer.NewLocalNotifier()
	cq, err := queue.NewChannelJobQueue(25, localNotifier.Notify)
	if err != nil {
		return err
	}

	dataDir := os.Getenv("ARKLOOP_DATA_DIR")
	if dataDir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return fmt.Errorf("resolve home dir: %w", herr)
		}
		dataDir = filepath.Join(home, ".arkloop")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}

	sqlitePath := filepath.Join(dataDir, "data.db")
	db, err := sqlitepgx.Open(sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	engine, err := app.ComposeDesktopEngine(ctx, db, bus, executor.DefaultExecutorRegistry())
	if err != nil {
		return fmt.Errorf("compose desktop engine: %w", err)
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
		nil,
		consumer.Config{
			Concurrency:      1,
			PollSeconds:      cfg.PollSeconds,
			LeaseSeconds:     cfg.LeaseSeconds,
			HeartbeatSeconds: cfg.HeartbeatSeconds,
			QueueJobTypes:    cfg.QueueJobTypes,
		},
		logger,
		localNotifier,
	)
	if err != nil {
		return err
	}

	logger.Info("desktop worker entering consume mode", app.LogFields{}, map[string]any{
		"concurrency": 1,
		"job_types":   cfg.QueueJobTypes,
		"sqlite_path": sqlitePath,
	})
	return loop.Run(ctx)
}

type desktopHandler struct {
	db     data.DesktopDB
	bus    eventbus.EventBus
	engine *app.DesktopEngine
	logger *app.JSONLogger
}

func (h *desktopHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	jobType, _ := lease.PayloadJSON["type"].(string)
	traceID, _ := lease.PayloadJSON["trace_id"].(string)
	runIDStr, _ := lease.PayloadJSON["run_id"].(string)

	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		return fmt.Errorf("parse run_id: %w", err)
	}

	fields := app.LogFields{
		JobID:   strPtr(lease.JobID.String()),
		TraceID: strPtr(traceID),
		RunID:   strPtr(runIDStr),
	}

	h.logger.Info("desktop handler received job", fields, map[string]any{"job_type": jobType})
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
		h.logger.Info("run not found, skipped", fields, nil)
		return nil
	}

	terminal, err := eventsRepo.GetLatestEventType(ctx, tx, runID, []string{
		"run.completed", "run.failed", "run.cancelled",
	})
	if err != nil {
		return fmt.Errorf("check terminal: %w", err)
	}
	if terminal != "" {
		h.logger.Info("run already terminal, skipped", fields, map[string]any{"terminal_type": terminal})
		return nil
	}

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

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if err := h.engine.Execute(ctx, *run, traceID); err != nil {
		slog.ErrorContext(ctx, "desktop engine execute failed", "run_id", runIDStr, "err", err)
		return err
	}

	h.publishEvent(ctx, "worker.job.completed", map[string]any{
		"job_id":   lease.JobID.String(),
		"job_type": jobType,
		"run_id":   runIDStr,
	})

	h.logger.Info("desktop handler completed job", fields, nil)
	return nil
}

func (h *desktopHandler) publishEvent(ctx context.Context, topic string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = h.bus.Publish(ctx, topic, string(raw))
}

func strPtr(v string) *string {
	s := v
	return &s
}

// desktopAgentExecutorBuilder wraps executor.Registry to satisfy pipeline.AgentExecutorBuilder.
var _ pipeline.AgentExecutorBuilder = (*executor.Registry)(nil)
