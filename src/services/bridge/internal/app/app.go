package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"arkloop/services/bridge/internal/audit"
	"arkloop/services/bridge/internal/docker"
	bridgehttp "arkloop/services/bridge/internal/http"
	"arkloop/services/bridge/internal/model"
	"arkloop/services/bridge/internal/module"
)

const (
	bridgeVersion     = "0.1.0"
	bridgeReadTimeout = 30 * time.Second
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

// logAdapter bridges slog.Logger to the simpler
// docker.Logger / bridgehttp.AppLogger interface (Info(msg, extra), Error(msg, extra)).
type logAdapter struct {
	logger *slog.Logger
}

func (a *logAdapter) Info(msg string, extra map[string]any) {
	a.logger.Info(msg, "extra", extra)
}
func (a *logAdapter) Error(msg string, extra map[string]any) {
	a.logger.Error(msg, "extra", extra)
}

func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load module registry.
	registry, err := module.LoadRegistry(a.config.ModulesFile)
	if err != nil {
		return fmt.Errorf("loading module registry: %w", err)
	}

	adapter := &logAdapter{logger: a.logger}

	// Create Docker Compose wrapper.
	compose := docker.NewCompose(a.config.ProjectDir, adapter)

	// Create operation store.
	operations := docker.NewOperationStore()

	// Create audit logger (file if configured, otherwise stdout).
	var auditWriter io.Writer = os.Stdout
	if a.config.AuditLog != "" {
		auditWriter = audit.NewRotatingFileWriter(a.config.AuditLog, 0, 0)
	}
	auditLog := audit.NewLogger(auditWriter)

	// Create model downloader for virtual modules (e.g. prompt-guard).
	modelDir := os.Getenv("ARKLOOP_PROMPT_GUARD_MODEL_DIR")
	modelDL := model.NewDownloader(modelDir, adapter)

	// Register routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)

	apiHandler := bridgehttp.NewHandler(registry, compose, operations, auditLog, adapter, modelDL, bridgeVersion)
	apiHandler.RegisterRoutes(mux)

	handler := corsMiddleware(a.config.CORSAllowedOrigins, mux)

	// Parse host and port from configured address.
	hostStr, portStr, err := net.SplitHostPort(a.config.Addr)
	if err != nil {
		return fmt.Errorf("parsing addr: %w", err)
	}

	// Determine listen addresses based on configured host.
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

	a.logger.Info("bridge started",
		"addr", a.config.Addr,
		"addr_v6", err6 == nil,
		"version", bridgeVersion,
		"project_dir", a.config.ProjectDir,
		"modules", a.config.ModulesFile,
	)

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       bridgeReadTimeout,
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- server.Serve(listener4)
	}()
	if err6 == nil {
		go func() {
			errCh <- server.Serve(listener6)
		}()
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

var healthzPayload = []byte(`{"status":"ok","version":"` + bridgeVersion + `"}`)
var healthzContentLength = strconv.Itoa(len(healthzPayload))

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"code":"http.method_not_allowed","message":"Method Not Allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", healthzContentLength)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(healthzPayload)
}

// corsMiddleware adds CORS headers for the configured allowed origins.
func corsMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[strings.TrimRight(o, "/")] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := origin == "null" || strings.HasPrefix(origin, "file://")
			if !allowed {
				normalized := strings.TrimRight(origin, "/")
				_, allowed = originSet[normalized]
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
