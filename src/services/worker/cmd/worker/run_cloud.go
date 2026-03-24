//go:build !desktop

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/eventbus"
	sharedlog "arkloop/services/shared/log"
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

func run() error {
	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	logger := sharedlog.New(sharedlog.Config{
		Component: "worker_go",
		Level:     slog.LevelInfo,
		Output:    os.Stdout,
	})
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

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	// advisory lock 每个并发 job 持有 1 个连接直到 job 完成，
	// 加上 pipeline 各阶段并发的 BeginTx，pool 大小必须足够：maxConcurrency * 3 + margin
	poolMinConns := int32(cfg.MaxConcurrency*3 + 8)
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
		dpCfg.MaxConns = 8
		dp, err := pgxpool.NewWithConfig(ctx, dpCfg)
		if err != nil {
			return fmt.Errorf("direct pool: %w", err)
		}
		defer dp.Close()
		directPool = dp
	} else {
		logger.Warn("ARKLOOP_DATABASE_DIRECT_URL not set: LISTEN/NOTIFY uses main pool, breaks with PgBouncer")
		directPool = pool
	}

	// cancel 必须在 pool.Close 之前执行，否则 LISTEN goroutine 会卡住 Close；
	// 重复调用 cancelRun 是幂等的，上面的 defer 保证 early return 也能 cancel。
	defer cancelRun()

	// Redis 在 chooseHandler 之前初始化，以便传入 run limiter
	var rdb *redis.Client
	if redisURL := lookupRedisURL(); redisURL != "" {
		rc, err := newRedisClient(runCtx, redisURL)
		if err != nil {
			logger.Error("redis connect failed, run limiter and registration disabled", "error", err.Error())
		} else {
			defer rc.Close()
			rdb = rc
		}
	} else {
		logger.Info("ARKLOOP_REDIS_URL not set, worker registration disabled")
	}

	var bus eventbus.EventBus
	if rdb != nil {
		bus = eventbus.NewRedisEventBus(rdb)
	} else {
		bus = eventbus.NewLocalEventBus()
	}

	var queueClient queue.JobQueue
	var notifier consumer.WorkNotifier

	if cfg.QueueDriver == "channel" {
		localNotifier := consumer.NewLocalNotifier()
		cq, err := queue.NewChannelJobQueue(25, localNotifier.Notify)
		if err != nil {
			return err
		}
		queueClient = cq
		notifier = localNotifier
		logger.Info("using channel job queue (in-process)")
	} else {
		pq, err := queue.NewPgQueue(pool, 25, cfg.Capabilities)
		if err != nil {
			return err
		}
		queueClient = pq

		if directPool != nil {
			pgNotifier := consumer.NewNotifier(directPool)
			pgNotifier.Start(runCtx)
			notifier = pgNotifier
		}
	}

	locker, err := consumer.NewPgAdvisoryLocker(pool)
	if err != nil {
		return err
	}

	handler, err := chooseHandler(runCtx, logger, pool, directPool, rdb, bus, queueClient, cfg)
	if err != nil {
		return err
	}

	var reg *registration.Manager
	if rdb != nil {
		reg, err = registration.NewManager(pool, rdb, registration.Config{
			Version:        cfg.Version,
			Capabilities:   cfg.Capabilities,
			MaxConcurrency: cfg.MaxConcurrency,
		}, logger)
		if err != nil {
			return fmt.Errorf("registration: %w", err)
		}
		if err := reg.Register(runCtx); err != nil {
			return fmt.Errorf("register worker: %w", err)
		}
		reg.StartHeartbeat(runCtx)
		handler = &loadAwareHandler{inner: handler, reg: reg}
	}

	loop, err := consumer.NewLoop(
		queueClient,
		handler,
		locker,
		consumer.Config{
			Concurrency:        cfg.Concurrency,
			PollSeconds:        cfg.PollSeconds,
			LeaseSeconds:       cfg.LeaseSeconds,
			HeartbeatSeconds:   cfg.HeartbeatSeconds,
			QueueJobTypes:      cfg.QueueJobTypes,
			MinConcurrency:     cfg.MinConcurrency,
			MaxConcurrency:     cfg.MaxConcurrency,
			ScaleUpThreshold:   cfg.ScaleUpThreshold,
			ScaleDownThreshold: cfg.ScaleDownThreshold,
			ScaleIntervalSecs:  cfg.ScaleIntervalSecs,
			ScaleCooldownSecs:  cfg.ScaleCooldownSecs,
		},
		logger,
		notifier,
	)
	if err != nil {
		return err
	}

	logger.Info("worker_go entering consume mode")
	runErr := loop.Run(runCtx)

	if reg != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := reg.Drain(shutCtx); err != nil {
			logger.Error("drain failed", "error", err.Error())
		}
		if err := reg.MarkDead(shutCtx); err != nil {
			logger.Error("mark dead failed", "error", err.Error())
		}
	}

	return runErr
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

func chooseHandler(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, directPool *pgxpool.Pool, rdb *redis.Client, bus eventbus.EventBus, q queue.JobQueue, cfg app.Config) (consumer.Handler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	configRegistry := sharedconfig.DefaultRegistry()
	var configCache sharedconfig.Cache
	cacheTTL := sharedconfig.CacheTTLFromEnv()
	if rdb != nil && cacheTTL > 0 {
		configCache = sharedconfig.NewRedisCache(rdb)
	}
	configResolver, _ := sharedconfig.NewResolver(configRegistry, sharedconfig.NewPGXStore(pool), configCache, cacheTTL)

	native, err := executor.NewNativeRunEngineV1Handler(ctx, pool, directPool, logger, rdb, q, cfg)
	if err != nil {
		return nil, err
	}
	logger.Info("worker_go native handler enabled", "job_type", queue.RunExecuteJobType)

	delivery, err := webhook.NewDeliveryHandler(pool, q, logger)
	if err != nil {
		return nil, err
	}
	logger.Info("webhook delivery handler enabled", "job_type", queue.WebhookDeliverJobType)

	emailHandler, err := email.NewSendHandler(configResolver, logger)
	if err != nil {
		return nil, err
	}
	emailHandler.SetSmtpProvider(email.NewPGSmtpProvider(pool))
	from, _ := configResolver.Resolve(context.Background(), "email.from", sharedconfig.Scope{})
	if strings.TrimSpace(from) != "" {
		logger.Info("email send handler enabled", "job_type", queue.EmailSendJobType, "from", strings.TrimSpace(from))
	} else {
		logger.Info("email send handler ready", "job_type", queue.EmailSendJobType)
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
