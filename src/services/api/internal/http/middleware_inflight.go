package http

import (
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/observability"
)

func InFlightMiddleware(next nethttp.Handler, maxInFlight int) nethttp.Handler {
	if next == nil {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", "", nil)
		})
	}
	if maxInFlight <= 0 {
		return next
	}

	sem := make(chan struct{}, maxInFlight)

	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if bypassInFlight(r) {
			next.ServeHTTP(w, r)
			return
		}

		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			traceID := observability.TraceIDFromContext(r.Context())
			WriteError(
				w,
				nethttp.StatusServiceUnavailable,
				"overload.busy",
				"service unavailable",
				traceID,
				map[string]any{"limit": maxInFlight},
			)
		}
	})
}

func bypassInFlight(r *nethttp.Request) bool {
	if r == nil {
		return true
	}
	if r.Method != nethttp.MethodGet {
		return false
	}

	path := r.URL.Path
	switch path {
	case "/healthz", "/readyz":
		return true
	}

	if strings.HasPrefix(path, "/v1/runs/") {
		if strings.HasSuffix(path, "/events") || strings.HasSuffix(path, "/events/") {
			return true
		}
	}

	return false
}
