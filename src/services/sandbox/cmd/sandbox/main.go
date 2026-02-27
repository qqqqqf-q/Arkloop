package main

import (
	"context"
	"os"
	"time"

	"arkloop/services/sandbox/internal/app"
	sandboxhttp "arkloop/services/sandbox/internal/http"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/snapshot"
	"arkloop/services/sandbox/internal/storage"
	"arkloop/services/sandbox/internal/template"
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

	// 可选依赖：MinIO 快照存储 + Template 注册表
	var snapshotStore storage.SnapshotStore
	var registry *template.Registry

	if cfg.S3Endpoint != "" {
		cacheDir := cfg.SocketBaseDir + "/_snapshots"
		store, err := storage.NewMinIOStore(context.Background(), cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cacheDir)
		if err != nil {
			return err
		}
		snapshotStore = store
	}

	if cfg.TemplatesPath != "" {
		reg, err := template.LoadFromFile(cfg.TemplatesPath)
		if err != nil {
			return err
		}
		registry = reg
	}

	// 启动时自动检查并构建缺失快照（带超时保护，超时则降级冷启动）
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

	mgr := session.NewManager(session.ManagerConfig{
		FirecrackerBin:     cfg.FirecrackerBin,
		KernelImagePath:    cfg.KernelImagePath,
		RootfsPath:         cfg.RootfsPath,
		SocketBaseDir:      cfg.SocketBaseDir,
		BootTimeoutSeconds: cfg.BootTimeoutSeconds,
		GuestAgentPort:     cfg.GuestAgentPort,
		MaxSessions:        cfg.MaxSessions,
		SnapshotStore:      snapshotStore,
		Registry:           registry,
	})

	handler := sandboxhttp.NewHandler(mgr, logger)

	application, err := app.NewApplication(cfg, logger, mgr)
	if err != nil {
		return err
	}
	return application.Run(context.Background(), handler)
}
