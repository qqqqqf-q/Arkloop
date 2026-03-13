package accountapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"errors"
	"mime"
	nethttp "net/http"
	"path"
	"strings"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/workspaceblob"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const workspaceRootPath = "/workspace"

func workspaceFilesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
	store environmentStore,
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

var errWorkspaceFileNotFound = errors.New("workspace file not found")

func parseWorkspaceRunID(w nethttp.ResponseWriter, traceID string, raw string) (uuid.UUID, bool) {
	runID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || runID == uuid.Nil {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_run_id", "invalid run_id", traceID, nil)
		return uuid.Nil, false
	}
	return runID, true
}

func normalizeWorkspaceRelativePath(w nethttp.ResponseWriter, traceID string, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
		return "", false
	}
	cleaned := path.Clean(path.Join(workspaceRootPath, strings.TrimPrefix(trimmed, "/")))
	if !strings.HasPrefix(cleaned, workspaceRootPath+"/") {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
		return "", false
	}
	return strings.TrimPrefix(strings.TrimPrefix(cleaned, workspaceRootPath), "/"), true
}

func workspaceManifestKey(workspaceRef, revision string) string {
	return "workspaces/" + workspaceRef + "/manifests/" + revision + ".json"
}

func workspaceBlobKey(workspaceRef, sha256 string) string {
	return "workspaces/" + workspaceRef + "/blobs/" + sha256
}

type workspaceManifest struct {
	Entries []workspaceManifestEntry `json:"entries,omitempty"`
}

type workspaceManifestEntry struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Size        int64  `json:"size,omitempty"`
	MtimeUnixMs int64  `json:"mtime_unix_ms,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`
}

const workspaceEntryTypeFile = "file"

func readWorkspaceFile(ctx context.Context, pool *pgxpool.Pool, store environmentStore, workspaceRef string, relativePath string) ([]byte, string, error) {
	return readWorkspaceFileFromManifest(ctx, pool, store, workspaceRef, relativePath)
}

func readWorkspaceFileFromManifest(ctx context.Context, pool *pgxpool.Pool, store environmentStore, workspaceRef string, relativePath string) ([]byte, string, error) {
	revision, err := loadWorkspaceManifestRevision(ctx, pool, workspaceRef)
	if err != nil {
		return nil, "", err
	}
	if revision == "" {
		return nil, "", errWorkspaceFileNotFound
	}
	manifestBytes, err := store.Get(ctx, workspaceManifestKey(workspaceRef, revision))
	if err != nil {
		if objectstore.IsNotFound(err) {
			return nil, "", errWorkspaceFileNotFound
		}
		return nil, "", err
	}
	var manifest workspaceManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, "", err
	}
	for _, entry := range manifest.Entries {
		if strings.TrimSpace(entry.Path) != strings.TrimSpace(relativePath) {
			continue
		}
		if entry.Type != workspaceEntryTypeFile || entry.Deleted || strings.TrimSpace(entry.SHA256) == "" {
			return nil, "", errWorkspaceFileNotFound
		}
		encoded, err := store.Get(ctx, workspaceBlobKey(workspaceRef, entry.SHA256))
		if err != nil {
			if objectstore.IsNotFound(err) {
				return nil, "", errWorkspaceFileNotFound
			}
			return nil, "", err
		}
		content, err := workspaceblob.Decode(encoded)
		if err != nil {
			return nil, "", err
		}
		return content, detectWorkspaceContentType(relativePath, content), nil
	}
	return nil, "", errWorkspaceFileNotFound
}

func loadWorkspaceManifestRevision(ctx context.Context, pool *pgxpool.Pool, workspaceRef string) (string, error) {
	if pool == nil {
		return "", errWorkspaceFileNotFound
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" {
		return "", errWorkspaceFileNotFound
	}
	var revision *string
	if err := pool.QueryRow(ctx, `SELECT latest_manifest_rev FROM workspace_registries WHERE workspace_ref = $1`, workspaceRef).Scan(&revision); err != nil {
		return "", errWorkspaceFileNotFound
	}
	if revision == nil {
		return "", nil
	}
	return strings.TrimSpace(*revision), nil
}
func detectWorkspaceContentType(relativePath string, content []byte) string {
	if ext := strings.ToLower(path.Ext(relativePath)); ext != "" {
		if guessed := mime.TypeByExtension(ext); strings.TrimSpace(guessed) != "" {
			return guessed
		}
	}
	return nethttp.DetectContentType(content)
}
