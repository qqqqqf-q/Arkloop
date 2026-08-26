package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"arkloop/services/bridge/internal/audit"
	"arkloop/services/bridge/internal/docker"
	bridgehttp "arkloop/services/bridge/internal/http"
	"arkloop/services/bridge/internal/module"
)

func (a *Application) RunDesktop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	registry, err := module.LoadRegistry(a.config.ModulesFile)
	if err != nil {
		return fmt.Errorf("loading module registry: %w", err)
	}

	adapter := &logAdapter{logger: a.logger}

	compose := docker.NewCompose(a.config.ProjectDir, adapter)
	operations := docker.NewOperationStore()

	var auditWriter io.Writer = os.Stdout
	if a.config.AuditLog != "" {
		auditWriter = audit.NewRotatingFileWriter(a.config.AuditLog, 0, 0)
	}
	auditLog := audit.NewLogger(auditWriter)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)

	apiHandler := bridgehttp.NewHandler(registry, compose, operations, auditLog, adapter, bridgeVersion)
	apiHandler.RegisterRoutes(mux)

	handler := bridgeHandler(a.config.AuthToken, a.config.CORSAllowedOrigins, mux)

	hostStr, portStr, err := net.SplitHostPort(a.config.Addr)
	if err != nil {
		return fmt.Errorf("parsing addr: %w", err)
	}

	v4Addr := "127.0.0.1:" + portStr
	v6Addr := "[::1]:" + portStr
	if hostStr == "0.0.0.0" {
		v4Addr = "0.0.0.0:" + portStr
		v6Addr = "[::]:" + portStr
	}

	listener4, err := net.Listen("tcp4", v4Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener4.Close() }()

	listener6, err6 := net.Listen("tcp6", v6Addr)
	if err6 == nil {
		defer func() { _ = listener6.Close() }()
	}

	slog.Info("bridge started (desktop)",
		"addr", a.config.Addr,
		"addr_v6", err6 == nil,
		"version", bridgeVersion,
		"project_dir", a.config.ProjectDir,
		"modules", a.config.ModulesFile,
	)

	srv := &http.Server{
		Handler:           handler,
		ReadTimeout:       bridgeReadTimeout,
		ReadHeaderTimeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	if err6 == nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Serve(listener4); err != nil && err != http.ErrServerClosed {
				slog.Error("bridge serve error", "error", err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Serve(listener6); err != nil && err != http.ErrServerClosed {
				slog.Error("bridge serve error (v6)", "error", err)
			}
		}()
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Serve(listener4); err != nil && err != http.ErrServerClosed {
				slog.Error("bridge serve error", "error", err)
			}
		}()
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	wg.Wait()

	return nil
}
