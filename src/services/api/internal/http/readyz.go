package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func readyz(schemaRepo *data.SchemaRepository, logger *slog.Logger) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			traceID := observability.TraceIDFromContext(r.Context())
			WriteError(w, nethttp.StatusMethodNotAllowed, "http.method_not_allowed", "Method Not Allowed", traceID, nil)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if schemaRepo == nil {
			if logger != nil {
				logger.ErrorContext(r.Context(), "readyz failed",
					"trace_id", traceID,
					"reason", "schemaRepo is nil",
				)
			}
			WriteError(w, nethttp.StatusServiceUnavailable, "health.not_ready", "service not ready", traceID, map[string]any{"dependency": "database"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		version, err := schemaRepo.CurrentSchemaVersion(ctx)
		if err != nil {
			if logger != nil {
				logger.ErrorContext(r.Context(), "readyz failed",
					"trace_id", traceID,
					"dependency", "database",
					"error", err.Error(),
				)
			}
			WriteError(
				w,
				nethttp.StatusServiceUnavailable,
				"health.not_ready",
				"service not ready",
				traceID,
				map[string]any{"dependency": "database"},
			)
			return
		}

		payload, err := json.Marshal(map[string]any{
			"status":         "ok",
			"schema_version": version,
		})
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write(payload)
	}
}
