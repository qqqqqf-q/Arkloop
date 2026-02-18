package http

import (
	"encoding/json"

	nethttp "net/http"

	"arkloop/services/api/internal/observability"
)

func healthz(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		traceID := observability.TraceIDFromContext(r.Context())
		WriteError(w, nethttp.StatusMethodNotAllowed, "http_error", "Method Not Allowed", traceID, nil)
		return
	}

	payload, err := json.Marshal(map[string]string{"status": "ok"})
	if err != nil {
		traceID := observability.TraceIDFromContext(r.Context())
		WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write(payload)
}
