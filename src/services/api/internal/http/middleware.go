package http

import (
	"fmt"
	"runtime/debug"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/httputil"
)

// requirePerm 校验 actor 是否拥有指定权限点。
// 无权或 actor 为 nil 时写 403 响应并返回 false，调用方应立即 return。
func requirePerm(a *httpkit.Actor, perm string, w nethttp.ResponseWriter, traceID string) bool {
	if a != nil && a.HasPermission(perm) {
		return true
	}
	WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "access denied", traceID, nil)
	return false
}

func TraceMiddleware(next nethttp.Handler, logger *observability.JSONLogger, trustIncomingTraceID bool, trustXFF bool) nethttp.Handler {
	if next == nil {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", "", nil)
		})
	}

	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		start := time.Now()

		traceID := observability.NewTraceID()
		if trustIncomingTraceID {
			if incoming := observability.NormalizeTraceID(r.Header.Get(observability.TraceIDHeader)); incoming != "" {
				traceID = incoming
			}
		}

		metadata := resolveRequestMetadata(r, trustXFF)
		ctx := observability.WithTraceID(r.Context(), traceID)
		ctx = observability.WithClientIP(ctx, metadata.clientIP)
		ctx = observability.WithRequestHTTPS(ctx, metadata.https)
		if ua := r.Header.Get("User-Agent"); ua != "" {
			ctx = observability.WithUserAgent(ctx, ua)
		}
		r = r.WithContext(ctx)

		recorder := &httputil.StatusRecorder{ResponseWriter: w, StatusCode: nethttp.StatusOK}
		recorder.Header().Set(observability.TraceIDHeader, traceID)

		next.ServeHTTP(recorder, r)

		if logger == nil {
			return
		}

		durationMs := time.Since(start).Milliseconds()
		logger.Info(
			"http request",
			observability.LogFields{TraceID: &traceID},
			map[string]any{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status_code": recorder.StatusCode,
				"duration_ms": durationMs,
			},
		)
	})
}

func RecoverMiddleware(next nethttp.Handler, logger *observability.JSONLogger) nethttp.Handler {
	if next == nil {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", "", nil)
		})
	}

	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		defer func() {
			if value := recover(); value != nil {
				traceID := observability.TraceIDFromContext(r.Context())
				if traceID == "" {
					traceID = observability.NewTraceID()
				}

				if logger != nil {
					logger.Error(
						"panic",
						observability.LogFields{TraceID: &traceID},
						map[string]any{
							"panic": fmt.Sprint(value),
							"stack": string(debug.Stack()),
						},
					)
				}

				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
