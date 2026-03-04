package http

import (
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/objectstore"
)

func artifactsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	store *objectstore.Store,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		if store == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "artifacts.not_configured", "artifact storage not configured", traceID, nil)
			return
		}

		a, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		// key 格式: {orgID}/{sessionID}/{filename}
		key := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
		if key == "" || strings.Contains(key, "..") {
			WriteError(w, nethttp.StatusBadRequest, "artifacts.invalid_key", "invalid artifact key", traceID, nil)
			return
		}

		// 校验 key 的 orgID 前缀与当前用户所属 org 一致
		slashIdx := strings.Index(key, "/")
		if slashIdx < 1 {
			WriteError(w, nethttp.StatusBadRequest, "artifacts.invalid_key", "invalid artifact key", traceID, nil)
			return
		}
		keyOrgID := key[:slashIdx]
		if keyOrgID != a.OrgID.String() {
			WriteError(w, nethttp.StatusForbidden, "artifacts.forbidden", "access denied", traceID, nil)
			return
		}

		blobData, contentType, err := store.GetWithContentType(r.Context(), key)
		if err != nil {
			WriteError(w, nethttp.StatusNotFound, "artifacts.not_found", "artifact not found", traceID, nil)
			return
		}

		if contentType == "" {
			contentType = "application/octet-stream"
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write(blobData)
	}
}
