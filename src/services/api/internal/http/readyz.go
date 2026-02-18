package http

import (
	"context"
	"encoding/json"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func readyz(schemaRepo *data.SchemaRepository, logger *observability.JSONLogger) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			traceID := observability.TraceIDFromContext(r.Context())
			WriteError(w, nethttp.StatusMethodNotAllowed, "http_error", "Method Not Allowed", traceID, nil)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if schemaRepo == nil {
			if logger != nil {
				logger.Error(
					"readyz 失败",
					observability.LogFields{TraceID: &traceID},
					map[string]any{"reason": "schemaRepo 为空"},
				)
			}
			WriteError(w, nethttp.StatusServiceUnavailable, "not_ready", "服务未就绪", traceID, map[string]any{"dependency": "database"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		revision, err := schemaRepo.CurrentAlembicVersion(ctx)
		if err != nil {
			if logger != nil {
				logger.Error(
					"readyz 失败",
					observability.LogFields{TraceID: &traceID},
					map[string]any{"dependency": "database", "error": err.Error()},
				)
			}
			WriteError(
				w,
				nethttp.StatusServiceUnavailable,
				"not_ready",
				"服务未就绪",
				traceID,
				map[string]any{"dependency": "database"},
			)
			return
		}

		payload, err := json.Marshal(map[string]string{
			"status":           "ok",
			"alembic_revision": revision,
		})
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write(payload)
	}
}
