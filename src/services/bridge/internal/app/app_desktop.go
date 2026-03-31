//go:build desktop

package app

import (
	"context"
	"encoding/json"
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
	"arkloop/services/bridge/internal/model"
	"arkloop/services/bridge/internal/module"
	"arkloop/services/shared/desktop"
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

	modelDir := os.Getenv("ARKLOOP_PROMPT_GUARD_MODEL_DIR")
	modelDL := model.NewDownloader(modelDir, adapter)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)

	apiHandler := bridgehttp.NewHandler(registry, compose, operations, auditLog, adapter, modelDL, bridgeVersion)
	apiHandler.RegisterRoutes(mux)

	// Desktop-only: execution-mode endpoint（模式由侧car main 从磁盘恢复，此处不再强制 local）
	// With the compose.yaml port mapping (127.0.0.1:19002), sandbox-docker
	// is reachable at this address if it is running.
	desktop.SetSandboxAddr("127.0.0.1:19002")
	slog.Debug("bridge desktop: sandbox addr set", "addr", desktop.GetSandboxAddr())
	mux.HandleFunc("GET /v1/execution-mode", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"mode": desktop.GetExecutionMode()})
	})
	mux.HandleFunc("POST /v1/execution-mode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.Mode != "local" && req.Mode != "vm" {
			http.Error(w, "mode must be local or vm", http.StatusBadRequest)
			return
		}
		oldMode := desktop.GetExecutionMode()
		desktop.SetExecutionMode(req.Mode)
		if err := desktop.PersistExecutionMode(req.Mode); err != nil {
			slog.Error("execution-mode: persist", "error", err)
		}
		slog.Debug("execution-mode POST", "remote", r.RemoteAddr, "mode", req.Mode, "was", oldMode)
		json.NewEncoder(w).Encode(map[string]string{"mode": req.Mode})
	})

	handler := corsMiddleware(a.config.CORSAllowedOrigins, mux)

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
