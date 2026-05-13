package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"arkloop/services/plugin-registry/internal/data"
	registryhttp "arkloop/services/plugin-registry/internal/http"
	"arkloop/services/plugin-registry/internal/storage"
	"arkloop/services/shared/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	serviceVersion    = "0.1.0"
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	bundleBucket      = "plugin-registry"
)

type Application struct {
	config Config
	logger *slog.Logger
}

func NewApplication(config Config, logger *slog.Logger) (*Application, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	return &Application{config: config, logger: logger}, nil
}

func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	repo, cleanup, err := a.openRepository(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	bundles, err := a.openBundleStore(ctx)
	if err != nil {
		return err
	}

	handler, err := registryhttp.NewHandler(repo, bundles, a.config.AdminToken)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	listener, err := net.Listen("tcp", a.config.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	a.logger.Info("plugin-registry started", "addr", a.config.Addr, "version", serviceVersion)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}
	err = <-errCh
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *Application) openRepository(ctx context.Context) (data.Repository, func(), error) {
	if a.config.DatabaseURL == "" {
		return data.NewMemoryRepository(), func() {}, nil
	}
	pool, err := pgxpool.New(ctx, a.config.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	if err := data.EnsureSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	return data.NewPostgresRepository(pool), pool.Close, nil
}

func (a *Application) openBundleStore(ctx context.Context) (storage.BundleStore, error) {
	if a.config.StorageDir == "" {
		return nil, nil
	}
	opener := objectstore.NewFilesystemOpener(a.config.StorageDir)
	store, err := opener.Open(ctx, bundleBucket)
	if err != nil {
		return nil, fmt.Errorf("open bundle store: %w", err)
	}
	return storage.NewObjectBundleStore(store), nil
}
