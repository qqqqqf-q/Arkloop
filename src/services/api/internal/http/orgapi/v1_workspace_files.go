package orgapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"errors"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/http/featuregate"
	"arkloop/services/api/internal/observability"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func workspaceFilesEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	runRepo *data.RunEventRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	store environmentStore,
	flagService *featureflag.Service,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}
		if store == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "workspace_files.not_configured", "workspace file storage not configured", traceID, nil)
			return
		}
		if pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataRunsRead, w, traceID) {
			return
		}

		runID, ok := parseWorkspaceRunID(w, traceID, r.URL.Query().Get("run_id"))
		if !ok {
			return
		}
		targetPath, ok := normalizeWorkspaceRelativePath(w, traceID, r.URL.Query().Get("path"))
		if !ok {
			return
		}

		run, err := runRepo.GetRun(r.Context(), runID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}
		if !authorizeRunOrAudit(w, r, traceID, actor, "workspace_files.get", run, auditWriter) {
			return
		}
		if !featuregate.EnsureClawEnabledForRun(w, traceID, r.Context(), run, threadRepo, flagService) {
			return
		}
		if run.WorkspaceRef == nil || strings.TrimSpace(*run.WorkspaceRef) == "" {
			httpkit.WriteError(w, nethttp.StatusNotFound, "workspace_files.not_found", "workspace file not found", traceID, nil)
			return
		}

		content, contentType, err := readWorkspaceFile(r.Context(), pool, store, strings.TrimSpace(*run.WorkspaceRef), targetPath)
		if err != nil {
			if errors.Is(err, errWorkspaceFileNotFound) {
				httpkit.WriteError(w, nethttp.StatusNotFound, "workspace_files.not_found", "workspace file not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=60")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write(content)
	}
}

func parseWorkspaceRunID(w nethttp.ResponseWriter, traceID string, raw string) (uuid.UUID, bool) {
	runID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || runID == uuid.Nil {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_run_id", "invalid run_id", traceID, nil)
		return uuid.Nil, false
	}
	return runID, true
}
