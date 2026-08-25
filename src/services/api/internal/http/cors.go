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

// isAllowedDesktopOrigin 检查 origin 是否来自本地环境。
// 允许: http://localhost[:port], http://127.0.0.1[:port], file://, app://, tauri://
func isAllowedDesktopOrigin(origin string) bool {
	lower := strings.ToLower(origin)

	// Electron file:// / app:// / tauri://
	for _, scheme := range []string{"file://", "app://", "tauri://"} {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}

	// http://localhost 或 http://127.0.0.1 (含可选端口)
	for _, prefix := range []string{"http://localhost", "http://127.0.0.1"} {
		if lower == prefix {
			return true
		}
		if strings.HasPrefix(lower, prefix+":") {
			return true
		}
	}

	return false
}

func desktopCORSMiddleware(next nethttp.Handler) nethttp.Handler {
	if next == nil {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {})
	}

	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		appendDesktopVaryHeader(w.Header(), "Origin")

		if origin != "" && isAllowedDesktopOrigin(origin) {
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
