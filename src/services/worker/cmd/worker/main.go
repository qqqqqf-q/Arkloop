package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/consumer"
	"arkloop/services/worker/internal/executor"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/registration"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}

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

	// Redis 在 chooseHandler 之前初始化，以便传入 run limiter
	var rdb *redis.Client
	if redisURL := lookupRedisURL(); redisURL != "" {
		rc, err := newRedisClient(ctx, redisURL)
		if err != nil {
			logger.Error("redis connect failed, run limiter and registration disabled", app.LogFields{}, map[string]any{"error": err.Error()})
		} else {
			defer rc.Close()
			rdb = rc
		}
	} else {
		logger.Info("ARKLOOP_REDIS_URL not set, worker registration disabled", app.LogFields{}, nil)
	}

	queueClient, err := queue.NewPgQueue(pool, 25, cfg.Capabilities)
	if err != nil {
		return err
	}
	locker, err := consumer.NewPgAdvisoryLocker(pool)
	if err != nil {
		return err
	}

	handler, err := chooseHandler(logger, pool, rdb, cfg.QueueJobTypes)
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

	var reg *registration.Manager
	if rdb != nil {
		reg, err = registration.NewManager(pool, rdb, registration.Config{
			Version:        cfg.Version,
			Capabilities:   cfg.Capabilities,
			MaxConcurrency: cfg.Concurrency,
		}, logger)
		if err != nil {
			return fmt.Errorf("registration: %w", err)
		}
		if err := reg.Register(ctx); err != nil {
			return fmt.Errorf("register worker: %w", err)
		}
		reg.StartHeartbeat(ctx)
		handler = &loadAwareHandler{inner: handler, reg: reg}
	}

	logger.Info("worker_go entering consume mode", app.LogFields{}, nil)
	runErr := loop.Run(ctx)

	if reg != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := reg.Drain(shutCtx); err != nil {
			logger.Error("drain failed", app.LogFields{}, map[string]any{"error": err.Error()})
		}
		if err := reg.MarkDead(shutCtx); err != nil {
			logger.Error("mark dead failed", app.LogFields{}, map[string]any{"error": err.Error()})
		}
	}

	return runErr
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

func lookupRedisURL() string {
	return strings.TrimSpace(os.Getenv("ARKLOOP_REDIS_URL"))
}

func newRedisClient(ctx context.Context, redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return client, nil
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

func chooseHandler(logger *app.JSONLogger, pool *pgxpool.Pool, rdb *redis.Client, queueJobTypes []string) (consumer.Handler, error) {
	_ = queueJobTypes
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}

	native, err := executor.NewNativeRunEngineV1Handler(pool, logger, rdb)
	if err != nil {
		return nil, err
	}
	logger.Info("worker_go native handler enabled", app.LogFields{}, map[string]any{"job_type": queue.RunExecuteJobType})
	return native, nil
}

// loadAwareHandler 在 job 处理前后更新注册管理器的负载计数。
type loadAwareHandler struct {
	inner consumer.Handler
	reg   *registration.Manager
}

func (h *loadAwareHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	h.reg.IncrLoad()
	defer h.reg.DecrLoad()
	return h.inner.Handle(ctx, lease)
}
