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
	"arkloop/services/worker/internal/email"
	"arkloop/services/worker/internal/executor"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/registration"
	"arkloop/services/worker/internal/webhook"

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

	// advisory lock 每个并发 job 持有 1 个连接直到 job 完成，
	// 加上 pipeline 各阶段并发的 BeginTx，pool 大小必须足够：concurrency * 3 + margin
	poolMinConns := int32(cfg.Concurrency*3 + 8)
	poolCfg, err := pgxpool.ParseConfig(normalizePostgresDSN(databaseDSN))
	if err != nil {
		return err
	}
	poolCfg.MaxConns = poolMinConns
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	// 直连 pool 用于 LISTEN/NOTIFY（绕过 PgBouncer transaction mode）；
	// 未设置时回退到 main pool 并打警告，PgBouncer 场景下 LISTEN 将失效。
	var directPool *pgxpool.Pool
	if directDSN := lookupDirectDatabaseDSN(); directDSN != "" {
		dpCfg, err := pgxpool.ParseConfig(normalizePostgresDSN(directDSN))
		if err != nil {
			return fmt.Errorf("direct pool config: %w", err)
		}
		dpCfg.MaxConns = int32(cfg.Concurrency + 2)
		dp, err := pgxpool.NewWithConfig(ctx, dpCfg)
		if err != nil {
			return fmt.Errorf("direct pool: %w", err)
		}
		defer dp.Close()
		directPool = dp
	} else {
		logger.Warn("ARKLOOP_DATABASE_DIRECT_URL not set: LISTEN/NOTIFY uses main pool, breaks with PgBouncer", app.LogFields{}, nil)
		directPool = pool
	}

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

	handler, err := chooseHandler(logger, pool, directPool, rdb, queueClient, cfg)
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

func lookupDirectDatabaseDSN() string {
	return strings.TrimSpace(os.Getenv("ARKLOOP_DATABASE_DIRECT_URL"))
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

func chooseHandler(logger *app.JSONLogger, pool *pgxpool.Pool, directPool *pgxpool.Pool, rdb *redis.Client, q queue.JobQueue, cfg app.Config) (consumer.Handler, error) {
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}

	native, err := executor.NewNativeRunEngineV1Handler(pool, directPool, logger, rdb, q, cfg)
	if err != nil {
		return nil, err
	}
	logger.Info("worker_go native handler enabled", app.LogFields{}, map[string]any{"job_type": queue.RunExecuteJobType})

	delivery, err := webhook.NewDeliveryHandler(pool, q, logger)
	if err != nil {
		return nil, err
	}
	logger.Info("webhook delivery handler enabled", app.LogFields{}, map[string]any{"job_type": queue.WebhookDeliverJobType})

	emailCfg, err := email.LoadConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("email config: %w", err)
	}
	mailer := email.NewMailer(emailCfg)
	emailHandler, err := email.NewSendHandler(mailer, logger)
	if err != nil {
		return nil, err
	}
	if emailCfg.Enabled() {
		logger.Info("email send handler enabled", app.LogFields{}, map[string]any{"job_type": queue.EmailSendJobType, "from": emailCfg.From})
	} else {
		logger.Info("email send handler using noop (ARKLOOP_EMAIL_FROM not set)", app.LogFields{}, nil)
	}

	return &dispatchHandler{
		handlers: map[string]consumer.Handler{
			queue.RunExecuteJobType: native,
			webhook.DeliverJobType:  delivery,
			queue.EmailSendJobType:  emailHandler,
		},
		fallback: native,
	}, nil
}

// dispatchHandler 按 job type 路由到对应 handler。
type dispatchHandler struct {
	handlers map[string]consumer.Handler
	fallback consumer.Handler
}

func (d *dispatchHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	jobType, _ := lease.PayloadJSON["type"].(string)
	if h, ok := d.handlers[jobType]; ok {
		return h.Handle(ctx, lease)
	}
	return d.fallback.Handle(ctx, lease)
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
