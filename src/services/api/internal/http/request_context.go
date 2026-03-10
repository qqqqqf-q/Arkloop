package http

import (
	"net"
	"net/http"
	"strings"

	"arkloop/services/api/internal/observability"
)

const forwardedProtoHeader = "X-Forwarded-Proto"

type requestMetadata struct {
	clientIP string
	https    bool
}

func resolveRequestMetadata(r *http.Request, trustForwarded bool) requestMetadata {
	if r == nil {
		return requestMetadata{}
	}
	return requestMetadata{
		clientIP: resolveClientIP(r, trustForwarded),
		https:    resolveRequestHTTPS(r, trustForwarded),
	}
}

// resolveClientIP 提取客户端真实 IP。
// 当 trustXFF=true（API 部署在 Gateway 后）时，从 X-Forwarded-For 取首个 IP。
// 直连时不信任 XFF，防止客户端伪造。
func resolveClientIP(r *http.Request, trustXFF bool) string {
	if trustXFF {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip, _, _ := strings.Cut(xff, ","); ip != "" {
				if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
					return parsed.String()
				}
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if parsed := net.ParseIP(strings.TrimSpace(r.RemoteAddr)); parsed != nil {
			return parsed.String()
		}
		return ""
	}
	return host
}

func resolveRequestHTTPS(r *http.Request, trustForwarded bool) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if !trustForwarded {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get(forwardedProtoHeader)), "https")
}

func requestClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if ip := observability.ClientIPFromContext(r.Context()); ip != "" {
		return ip
	}
	return resolveClientIP(r, false)
}

func requestHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if enabled, ok := observability.RequestHTTPSFromContext(r.Context()); ok {
		return enabled
	}
	return r.TLS != nil
}
