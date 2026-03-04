package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"arkloop/services/sandbox/internal/app"
	dockerpool "arkloop/services/sandbox/internal/docker"
	sandboxhttp "arkloop/services/sandbox/internal/http"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/pool"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/snapshot"
	"arkloop/services/sandbox/internal/storage"
	"arkloop/services/sandbox/internal/template"
	"arkloop/services/shared/objectstore"
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

	logger := logging.NewJSONLogger("sandbox", os.Stdout)

	// 可选依赖：S3 artifact 存储
	var artifactStore *objectstore.Store
	if cfg.S3Endpoint != "" {
		aStore, err := objectstore.New(context.Background(), cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, objectstore.ArtifactBucket, "")
		if err != nil {
			return err
		}
		artifactStore = aStore
		logger.Info("artifact store initialized", logging.LogFields{}, nil)
	}

	var vmPool session.VMPool

	switch cfg.Provider {
	case app.ProviderFirecracker:
		vmPool, err = buildFirecrackerPool(cfg, logger)
	case app.ProviderDocker:
		vmPool, err = buildDockerPool(cfg, logger)
	default:
		err = fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
	if err != nil {
		return err
	}

	mgr := session.NewManager(session.ManagerConfig{
		MaxSessions:        cfg.MaxSessions,
		Pool:               vmPool,
		IdleTimeoutLite:    cfg.IdleTimeoutLite,
		IdleTimeoutPro:     cfg.IdleTimeoutPro,
		IdleTimeoutUltra:   cfg.IdleTimeoutUltra,
		MaxLifetimeSeconds: cfg.MaxLifetimeSeconds,
	})

	handler := sandboxhttp.NewHandler(mgr, artifactStore, logger, cfg.AuthToken)

	if cfg.AuthToken == "" {
		logger.Warn("ARKLOOP_SANDBOX_AUTH_TOKEN not set, auth disabled", logging.LogFields{}, nil)
	}

	application, err := app.NewApplication(cfg, logger, mgr)
	if err != nil {
		return err
	}
	return application.Run(context.Background(), handler)
}

func buildFirecrackerPool(cfg app.Config, logger *logging.JSONLogger) (session.VMPool, error) {
	var snapshotStore storage.SnapshotStore
	var registry *template.Registry

	if cfg.S3Endpoint != "" {
		cacheDir := cfg.SocketBaseDir + "/_snapshots"
		store, err := storage.NewMinIOStore(context.Background(), cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cacheDir)
		if err != nil {
			return nil, err
		}
		snapshotStore = store
	}

	if cfg.TemplatesPath != "" {
		reg, err := template.LoadFromFile(cfg.TemplatesPath)
		if err != nil {
			return nil, err
		}
		registry = reg
	}

	if snapshotStore != nil && registry != nil {
		builder := snapshot.NewBuilder(
			cfg.FirecrackerBin,
			cfg.SocketBaseDir,
			cfg.BootTimeoutSeconds,
			cfg.GuestAgentPort,
			snapshotStore,
			logger,
		)
		ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := builder.EnsureAll(ensureCtx, registry); err != nil {
			logger.Warn("snapshot ensure incomplete, falling back to cold boot", logging.LogFields{}, map[string]any{"error": err.Error()})
		}
		ensureCancel()
	}

	warmPool := pool.New(pool.Config{
		WarmSizes:             cfg.WarmSizes(),
		RefillIntervalSeconds: cfg.RefillIntervalSeconds,
		MaxRefillConcurrency:  cfg.RefillConcurrency,
		FirecrackerBin:        cfg.FirecrackerBin,
		KernelImagePath:       cfg.KernelImagePath,
		RootfsPath:            cfg.RootfsPath,
		SocketBaseDir:         cfg.SocketBaseDir,
		BootTimeoutSeconds:    cfg.BootTimeoutSeconds,
		GuestAgentPort:        cfg.GuestAgentPort,
		SnapshotStore:         snapshotStore,
		Registry:              registry,
		Logger:                logger,
	})
	warmPool.Start()

	return warmPool, nil
}

func buildDockerPool(cfg app.Config, logger *logging.JSONLogger) (session.VMPool, error) {
	dp, err := dockerpool.New(dockerpool.Config{
		WarmSizes:             cfg.WarmSizes(),
		RefillIntervalSeconds: cfg.RefillIntervalSeconds,
		MaxRefillConcurrency:  cfg.RefillConcurrency,
		Image:                 cfg.DockerImage,
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
