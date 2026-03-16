//go:build desktop

package http

import (
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/observability"
)

const (
	desktopCORSAllowMethods = "GET,POST,PUT,PATCH,DELETE,OPTIONS"
	desktopCORSAllowHeaders = "Authorization,Content-Type,Accept,X-Client-App,X-Trace-Id"
)

func appendDesktopVaryHeader(header nethttp.Header, value string) {
	if header == nil || value == "" {
		return
	}
	for _, item := range header.Values("Vary") {
		for _, part := range strings.Split(item, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func desktopCORSMiddleware(next nethttp.Handler) nethttp.Handler {
	if next == nil {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {})
	}

	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			appendDesktopVaryHeader(w.Header(), "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", desktopCORSAllowMethods)
			w.Header().Set("Access-Control-Allow-Headers", desktopCORSAllowHeaders)
			w.Header().Set("Access-Control-Expose-Headers", observability.TraceIDHeader)
			if r.Method == nethttp.MethodOptions {
				w.WriteHeader(nethttp.StatusNoContent)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
