package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/consumer"
	"arkloop/services/worker/internal/executor"
	"arkloop/services/worker/internal/queue"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	logger := app.NewJSONLogger("worker_go", os.Stdout)
	databaseDSN := lookupDatabaseDSN()
	if databaseDSN == "" {
		application, err := app.NewApplication(cfg, logger)
		if err != nil {
			return err
		}
		return application.Run(context.Background())
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, normalizePostgresDSN(databaseDSN))
	if err != nil {
		return err
	}
	defer pool.Close()

	queueClient, err := queue.NewPgQueue(pool, 25)
	if err != nil {
		return err
	}
	locker, err := consumer.NewPgAdvisoryLocker(pool)
	if err != nil {
		return err
	}

	handler, err := chooseHandler(logger, pool, cfg.QueueJobTypes)
	if err != nil {
		return err
	}

	loop, err := consumer.NewLoop(
		queueClient,
		handler,
		locker,
		consumer.Config{
			Concurrency:      cfg.Concurrency,
			PollSeconds:      cfg.PollSeconds,
			LeaseSeconds:     cfg.LeaseSeconds,
			HeartbeatSeconds: cfg.HeartbeatSeconds,
			QueueJobTypes:    cfg.QueueJobTypes,
		},
		logger,
	)
	if err != nil {
		return err
	}

	logger.Info("worker_go 进入消费模式", app.LogFields{}, nil)
	return loop.Run(ctx)
}

func lookupDatabaseDSN() string {
	for _, key := range []string{"ARKLOOP_DATABASE_URL", "DATABASE_URL"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizePostgresDSN(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Scheme == "postgresql+asyncpg" {
		parsed.Scheme = "postgresql"
		return parsed.String()
	}
	if strings.HasPrefix(parsed.Scheme, "postgresql") || parsed.Scheme == "postgres" {
		return parsed.String()
	}
	_, _ = os.Stderr.WriteString(fmt.Sprintf("warning: unknown postgres scheme %q, keep original dsn\n", parsed.Scheme))
	return raw
}

func chooseHandler(logger *app.JSONLogger, pool *pgxpool.Pool, queueJobTypes []string) (consumer.Handler, error) {
	_ = queueJobTypes
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}
	if pool == nil {
		return nil, fmt.Errorf("pool 不能为空")
	}

	native, err := executor.NewNativeRunEngineV1Handler(pool, logger)
	if err != nil {
		return nil, err
	}
	logger.Info("worker_go 已启用 native handler", app.LogFields{}, map[string]any{"job_type": queue.RunExecuteJobType})
	return native, nil
}
