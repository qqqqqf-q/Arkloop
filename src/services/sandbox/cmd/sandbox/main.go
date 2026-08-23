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

	"arkloop/services/sandbox/internal/app"
	dockerpool "arkloop/services/sandbox/internal/docker"
	"arkloop/services/sandbox/internal/environment"
	sandboxhttp "arkloop/services/sandbox/internal/http"
	"arkloop/services/sandbox/internal/logging"
	processsvc "arkloop/services/sandbox/internal/process"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/shell"
	"arkloop/services/sandbox/internal/skills"
	sharedlog "arkloop/services/shared/log"
	"arkloop/services/shared/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
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

	// 统一 slog 输出格式
	slog.SetDefault(sharedlog.New(sharedlog.Config{
		Component: "sandbox",
		Level:     slog.LevelDebug,
		Output:    os.Stdout,
	}))

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	logger := logging.NewJSONLogger("sandbox", os.Stdout)

	// 可选依赖：artifact 存储
	var artifactStore objectstore.Store
	var stateStore objectstore.Store
	var envStore objectstore.BlobStore
	var skillStore objectstore.Store
	bucketOpener, err := buildStorageBucketOpener(cfg)
	if err != nil {
		return err
	}
	if bucketOpener != nil {
		aStore, err := bucketOpener.Open(context.Background(), objectstore.ArtifactBucket)
		if err != nil {
			return err
		}
		artifactStore = aStore
		sStore, err := bucketOpener.Open(context.Background(), objectstore.SessionStateBucket)
		if err != nil {
			return err
		}
		if err := applyStateStoreLifecycle(context.Background(), cfg, sStore); err != nil {
			return err
		}
		stateStore = sStore
		eStore, err := bucketOpener.Open(context.Background(), objectstore.EnvironmentStateBucket)
		if err != nil {
			return err
		}
		blobStore, ok := eStore.(objectstore.BlobStore)
		if !ok {
			return fmt.Errorf("environment store does not implement blob store")
		}
		envStore = blobStore
		kStore, err := bucketOpener.Open(context.Background(), objectstore.SkillStoreBucket)
		if err != nil {
			return err
		}
		skillStore = kStore
		logger.Info("artifact store initialized", logging.LogFields{}, nil)
	}

	var vmPool session.VMPool

	switch cfg.Provider {
	case app.ProviderDocker:
		vmPool, err = buildDockerPool(cfg, logger)
	case app.ProviderVz:
		vmPool, err = buildVzPool(cfg, logger)
	default:
		err = fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
	if err != nil {
		return err
	}

	dbPool, err := openRegistryPool()
	if err != nil {
		return err
	}
	if dbPool != nil {
		defer dbPool.Close()
	}
	registryWriter := environment.NewPGRegistryWriter(dbPool)
	restoreRegistry := shell.NewPGSessionRestoreRegistry(dbPool)

	mgr := session.NewManager(session.ManagerConfig{
		MaxSessions: cfg.MaxSessions,
		Pool:        vmPool,
		IdleTimeouts: map[string]int{
			session.TierLite:    cfg.IdleTimeoutSeconds(session.TierLite),
			session.TierPro:     cfg.IdleTimeoutSeconds(session.TierPro),
			session.TierBrowser: cfg.IdleTimeoutSeconds(session.TierBrowser),
		},
		MaxLifetimes: map[string]int{
			session.TierLite:    cfg.MaxLifetimeSecondsFor(session.TierLite),
			session.TierPro:     cfg.MaxLifetimeSecondsFor(session.TierPro),
			session.TierBrowser: cfg.MaxLifetimeSecondsFor(session.TierBrowser),
		},
	})
	envMgr := environment.NewManager(envStore, registryWriter, logger, environment.Config{
		DebounceDelay:       time.Duration(cfg.FlushDebounceMS) * time.Millisecond,
		MaxDirtyAge:         time.Duration(cfg.FlushMaxDirtyAgeMS) * time.Millisecond,
		ForceBytesThreshold: int64(cfg.FlushForceBytesThreshold),
		ForceCountThreshold: cfg.FlushForceCountThreshold,
	})
	skillOverlay := skills.NewOverlayManager(skillStore)
	shellMgr := shell.NewManager(mgr, artifactStore, stateStore, restoreRegistry, envMgr, skillOverlay, logger, shell.Config{
		RestoreTTL: time.Duration(cfg.RestoreTTLDays) * 24 * time.Hour,
	})
	processMgr := processsvc.NewManager(mgr, artifactStore, envMgr, skillOverlay, logger)

	handler := sandboxhttp.NewHandler(mgr, envMgr, skillOverlay, shellMgr, processMgr, artifactStore, logger, cfg.AuthToken)

	if cfg.AuthToken == "" {
		return fmt.Errorf("ARKLOOP_SANDBOX_AUTH_TOKEN must be set, refusing to start without auth")
	}

	application, err := app.NewApplication(cfg, logger, mgr)
	if err != nil {
		return err
	}
	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	envMgr.StartGovernance(runCtx)
	shellMgr.StartGovernance(runCtx)
	return application.Run(runCtx, handler)
}

func applyStateStoreLifecycle(ctx context.Context, cfg app.Config, store any) error {
	if store == nil || cfg.RestoreTTLDays == 0 {
		return nil
	}
	configurer, ok := store.(objectstore.LifecycleConfigurator)
	if !ok {
		return nil
	}
	if err := configurer.SetLifecycleExpirationDays(ctx, cfg.RestoreTTLDays); err != nil {
		return fmt.Errorf("configure session state lifecycle: %w", err)
	}
	return nil
}

func buildStorageBucketOpener(cfg app.Config) (objectstore.BucketOpener, error) {
	runtimeConfig, err := objectstore.NormalizeRuntimeConfig(objectstore.RuntimeConfig{
		Backend: cfg.StorageBackend,
		RootDir: cfg.StorageRoot,
		S3Config: objectstore.S3Config{
			Endpoint:  cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	if !runtimeConfig.Enabled() {
		return nil, nil
	}
	return runtimeConfig.BucketOpener()
}

func buildDockerPool(cfg app.Config, logger *logging.JSONLogger) (session.VMPool, error) {
	dp, err := dockerpool.New(dockerpool.Config{
		WarmSizes:             cfg.WarmSizes(),
		RefillIntervalSeconds: cfg.RefillIntervalSeconds,
		MaxRefillConcurrency:  cfg.RefillConcurrency,
		Image:                 cfg.DockerImage,
		BrowserImage:          cfg.BrowserDockerImage,
		AllowEgress:           cfg.AllowEgress,
		NetworkName:           cfg.DockerNetwork,
		GuestAgentPort:        cfg.GuestAgentPort,
		SocketBaseDir:         cfg.SocketBaseDir,
		Logger:                logger,
	})
	if err != nil {
		return nil, err
	}

	ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer ensureCancel()
	if err := dp.EnsureImage(ensureCtx); err != nil {
		return nil, err
	}

	dp.Start()
	return dp, nil
}

func openRegistryPool() (*pgxpool.Pool, error) {
	databaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_DATABASE_URL"))
	if databaseURL == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = 4
	return pgxpool.NewWithConfig(ctx, poolCfg)
}
