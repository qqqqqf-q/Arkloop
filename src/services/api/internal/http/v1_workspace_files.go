package http

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	nethttp "net/http"
	"path"
	"strings"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/objectstore"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
)

const workspaceRootPath = "/workspace"

func workspaceFilesEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	store environmentStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if runRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}
		if store == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "workspace_files.not_configured", "workspace file storage not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermDataRunsRead, w, traceID) {
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
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if run == nil {
			WriteError(w, nethttp.StatusNotFound, "runs.not_found", "run not found", traceID, nil)
			return
		}
		if !authorizeRunOrAudit(w, r, traceID, actor, "workspace_files.get", run, auditWriter) {
			return
		}
		if run.WorkspaceRef == nil || strings.TrimSpace(*run.WorkspaceRef) == "" {
			WriteError(w, nethttp.StatusNotFound, "workspace_files.not_found", "workspace file not found", traceID, nil)
			return
		}

		content, contentType, err := readWorkspaceFile(r.Context(), store, strings.TrimSpace(*run.WorkspaceRef), targetPath)
		if err != nil {
			if errors.Is(err, errWorkspaceFileNotFound) {
				WriteError(w, nethttp.StatusNotFound, "workspace_files.not_found", "workspace file not found", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
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
		WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_run_id", "invalid run_id", traceID, nil)
		return uuid.Nil, false
	}
	return runID, true
}

func normalizeWorkspaceRelativePath(w nethttp.ResponseWriter, traceID string, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
		return "", false
	}
	relative := strings.TrimPrefix(trimmed, "/")
	relative = strings.TrimPrefix(relative, "workspace/")
	if strings.EqualFold(relative, "workspace") {
		relative = ""
	}
	cleaned := path.Clean(path.Join(workspaceRootPath, relative))
	if cleaned != workspaceRootPath && !strings.HasPrefix(cleaned, workspaceRootPath+"/") {
		WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
		return "", false
	}
	relative = strings.TrimPrefix(strings.TrimPrefix(cleaned, workspaceRootPath), "/")
	if relative == "" {
		WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
		return "", false
	}
	return relative, true
}

func workspaceArchiveKey(workspaceRef string) string {
	return "workspaces/" + workspaceRef + "/state.tar.zst"
}

func workspaceLatestKey(workspaceRef string) string {
	return "workspaces/" + workspaceRef + "/latest.json"
}

func workspaceManifestKey(workspaceRef, revision string) string {
	return "workspaces/" + workspaceRef + "/manifests/" + revision + ".json"
}

func workspaceBlobKey(workspaceRef, sha256 string) string {
	return "workspaces/" + workspaceRef + "/blobs/" + sha256
}

type workspaceLatestPointer struct {
	Revision string `json:"revision"`
}

type workspaceManifest struct {
	Entries []workspaceManifestEntry `json:"entries,omitempty"`
}

type workspaceManifestEntry struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	SHA256  string `json:"sha256,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

const workspaceEntryTypeFile = "file"

func readWorkspaceFile(ctx context.Context, store environmentStore, workspaceRef string, relativePath string) ([]byte, string, error) {
	content, contentType, err := readWorkspaceFileFromManifest(ctx, store, workspaceRef, relativePath)
	if err == nil {
		return content, contentType, nil
	}
	if !errors.Is(err, errWorkspaceFileNotFound) {
		return nil, "", err
	}
	archive, archiveErr := store.Get(ctx, workspaceArchiveKey(workspaceRef))
	if archiveErr != nil {
		if objectstore.IsNotFound(archiveErr) {
			return nil, "", errWorkspaceFileNotFound
		}
		return nil, "", archiveErr
	}
	return readWorkspaceFileFromArchive(archive, relativePath)
}

func readWorkspaceFileFromManifest(ctx context.Context, store environmentStore, workspaceRef string, relativePath string) ([]byte, string, error) {
	pointerBytes, err := store.Get(ctx, workspaceLatestKey(workspaceRef))
	if err != nil {
		if objectstore.IsNotFound(err) {
			return nil, "", errWorkspaceFileNotFound
		}
		return nil, "", err
	}
	var pointer workspaceLatestPointer
	if err := json.Unmarshal(pointerBytes, &pointer); err != nil {
		return nil, "", err
	}
	revision := strings.TrimSpace(pointer.Revision)
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
		content, err := store.Get(ctx, workspaceBlobKey(workspaceRef, entry.SHA256))
		if err != nil {
			if objectstore.IsNotFound(err) {
				return nil, "", errWorkspaceFileNotFound
			}
			return nil, "", err
		}
		return content, detectWorkspaceContentType(relativePath, content), nil
	}
	return nil, "", errWorkspaceFileNotFound
}

func readWorkspaceFileFromArchive(archive []byte, relativePath string) ([]byte, string, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, "", fmt.Errorf("open zstd archive: %w", err)
	}
	defer decoder.Close()

	targetName := strings.TrimPrefix(path.Join(workspaceRootPath, relativePath), "/")
	tr := tar.NewReader(decoder)
	for {
		header, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "", errWorkspaceFileNotFound
			}
			return nil, "", fmt.Errorf("iterate tar archive: %w", err)
		}
		if header == nil {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		headerName := path.Clean(strings.TrimPrefix(header.Name, "/"))
		if headerName != targetName {
			continue
		}
		content := make([]byte, header.Size)
		if _, err := io.ReadFull(tr, content); err != nil {
			return nil, "", fmt.Errorf("read workspace file: %w", err)
		}
		return content, detectWorkspaceContentType(relativePath, content), nil
	}
}

func detectWorkspaceContentType(relativePath string, content []byte) string {
	if ext := strings.ToLower(path.Ext(relativePath)); ext != "" {
		if guessed := mime.TypeByExtension(ext); strings.TrimSpace(guessed) != "" {
			return guessed
		}
	}
	return nethttp.DetectContentType(content)
}
