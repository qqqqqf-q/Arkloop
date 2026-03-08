package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"arkloop/services/sandbox/internal/environment"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/shell"
	"arkloop/services/shared/objectstore"
)

// NewHandler 注册所有路由并返回 HTTP handler。
func NewHandler(mgr *session.Manager, envMgr *environment.Manager, shellSvc shell.Service, artifactStore *objectstore.Store, logger *logging.JSONLogger, authToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz(mgr))
	mux.HandleFunc("GET /v1/stats", stats(mgr))
	mux.HandleFunc("POST /v1/exec", handleExec(mgr, envMgr, artifactStore, logger))
	mux.HandleFunc("POST /v1/exec_command", handleExecCommand(shellSvc, logger))
	mux.HandleFunc("POST /v1/write_stdin", handleWriteStdin(shellSvc, logger))
	mux.HandleFunc("POST /v1/sessions/fork", handleForkSession(shellSvc))
	mux.HandleFunc("GET /v1/sessions/", handleSessionTranscript(shellSvc))
	mux.HandleFunc("DELETE /v1/sessions/", handleDeleteSession(mgr, shellSvc, logger))
	return recoverMiddleware(authMiddleware(mux, authToken, logger), logger)
}

func healthz(mgr *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		active := mgr.ActiveCount()
		poolReady := mgr.PoolReady()

		status := "ok"
		if !poolReady {
			status = "starting"
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":          status,
			"sessions":        active,
			"warm_pool_ready": poolReady,
		})
	}
}

func stats(mgr *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolStats := mgr.PoolStats()

		warmPool := make(map[string]any)
		for tier, target := range poolStats.TargetByTier {
			ready := poolStats.ReadyByTier[tier]
			warmPool[tier] = map[string]any{
				"ready":  ready,
				"target": target,
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"active_sessions":         mgr.ActiveCount(),
			"sessions_by_tier":        mgr.SessionsByTier(),
			"warm_pool":               warmPool,
			"total_created":           poolStats.TotalCreated,
			"total_destroyed":         poolStats.TotalDestroyed,
			"total_timeout_reclaimed": mgr.TotalReclaimed(),
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"code":"internal.error","message":"marshal failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"code":    code,
		"message": message,
	})
}

func recoverMiddleware(next http.Handler, logger *logging.JSONLogger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("panic recovered", logging.LogFields{}, map[string]any{"panic": fmt.Sprintf("%v", v)})
				writeError(w, http.StatusInternalServerError, "internal.panic", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// authMiddleware 对业务端点做 Bearer token 校验，healthz 免认证。
func authMiddleware(next http.Handler, token string, logger *logging.JSONLogger) http.Handler {
	enforced := token != ""
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !enforced {
			next.ServeHTTP(w, r)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if provided != token {
			writeError(w, http.StatusUnauthorized, "sandbox.unauthorized", "invalid or missing auth token")
			return
		}
		next.ServeHTTP(w, r)
	})
}
