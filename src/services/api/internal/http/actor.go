package http

import (
	"strings"

	nethttp "net/http"

	httpkit "arkloop/services/api/internal/http/httpkit"

	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type actor struct {
	AccountID   uuid.UUID
	UserID      uuid.UUID
	AccountRole string
	Permissions []string
}

func (a *actor) HasPermission(perm string) bool {
	for _, p := range a.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

func writeNotFound(w nethttp.ResponseWriter, r *nethttp.Request) {
	traceID := observability.TraceIDFromContext(r.Context())
	WriteError(w, nethttp.StatusNotFound, "http.method_not_allowed", "Not Found", traceID, nil)
}

func parseBearerToken(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if strings.TrimSpace(authorization) == "" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.missing_token", "missing Authorization Bearer token", traceID, nil)
		return "", false
	}
	scheme, rest, ok := strings.Cut(authorization, " ")
	if !ok || strings.TrimSpace(rest) == "" || strings.ToLower(scheme) != "bearer" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_authorization", "Authorization header must be: Bearer <token>", traceID, nil)
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func writeAuthNotConfigured(w nethttp.ResponseWriter, traceID string) {
	httpkit.WriteAuthNotConfigured(w, traceID)
}
