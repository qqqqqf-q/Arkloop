package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	nethttp "net/http"
	"path"
	"sort"
	"strings"

	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/objectstore"

)

var errWorkspacePathInvalid = errors.New("invalid workspace path")

const (
	workspaceEntryTypeDir     = "dir"
	workspaceEntryTypeSymlink = "symlink"
)

func normalizeWorkspaceDirectoryPath(w nethttp.ResponseWriter, traceID string, raw string) (string, bool) {
	relativePath, err := normalizeWorkspacePath(raw, true)
	if err != nil {
		writeInvalidWorkspacePath(w, traceID)
		return "", false
	}
	return relativePath, true
}

func normalizeWorkspacePath(raw string, allowRoot bool) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if allowRoot {
			return "", nil
		}
		return "", errWorkspacePathInvalid
	}

	cleaned := path.Clean(path.Join(workspaceRootPath, strings.TrimPrefix(trimmed, "/")))
	if cleaned == workspaceRootPath {
		if allowRoot {
			return "", nil
		}
		return "", errWorkspacePathInvalid
	}
	if !strings.HasPrefix(cleaned, workspaceRootPath+"/") {
		return "", errWorkspacePathInvalid
	}
	return strings.TrimPrefix(strings.TrimPrefix(cleaned, workspaceRootPath), "/"), nil
}

func writeInvalidWorkspacePath(w nethttp.ResponseWriter, traceID string) {
	httpkit.WriteError(w, nethttp.StatusBadRequest, "workspace_files.invalid_path", "invalid workspace path", traceID, nil)
}

func displayWorkspacePath(relativePath string) string {
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" {
		return "/"
	}
	return "/" + relativePath
}

func loadWorkspaceManifest(ctx context.Context, db data.DB, store environmentStore, workspaceRef string) (workspaceManifest, error) {
	revision, err := loadWorkspaceManifestRevision(ctx, db, workspaceRef)
	if err != nil {
		return workspaceManifest{}, err
	}
	if revision == "" {
		return workspaceManifest{}, nil
	}
	manifestBytes, err := store.Get(ctx, workspaceManifestKey(workspaceRef, revision))
	if err != nil {
		if objectstore.IsNotFound(err) {
			return workspaceManifest{}, errWorkspaceFileNotFound
		}
		return workspaceManifest{}, err
	}
	var manifest workspaceManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return workspaceManifest{}, err
	}
	return manifest, nil
}

func listWorkspaceManifestEntries(
	ctx context.Context,
	db data.DB,
	store environmentStore,
	workspaceRef string,
	relativePath string,
) ([]projectWorkspaceFileListItem, error) {
	manifest, err := loadWorkspaceManifest(ctx, db, store, workspaceRef)
	if err != nil {
		return nil, err
	}
	return buildWorkspaceFileList(manifest, relativePath), nil
}

func buildWorkspaceFileList(manifest workspaceManifest, relativePath string) []projectWorkspaceFileListItem {
	if len(manifest.Entries) == 0 {
		return []projectWorkspaceFileListItem{}
	}

	itemsByPath := make(map[string]*projectWorkspaceFileListItem)
	prefix := ""
	if relativePath != "" {
		prefix = relativePath + "/"
	}
	for _, entry := range manifest.Entries {
		entryPath := strings.Trim(strings.TrimSpace(entry.Path), "/")
		if entryPath == "" || entry.Deleted {
			continue
		}
		if relativePath != "" {
			if entryPath == relativePath {
				continue
			}
			if !strings.HasPrefix(entryPath, prefix) {
				continue
			}
		}

		remainder := entryPath
		if prefix != "" {
			remainder = strings.TrimPrefix(entryPath, prefix)
		}
		if remainder == "" {
			continue
		}
		childName, childTail, hasMore := strings.Cut(remainder, "/")
		childPath := childName
		if relativePath != "" {
			childPath = relativePath + "/" + childName
		}
		item, ok := itemsByPath[childPath]
		if !ok {
			item = &projectWorkspaceFileListItem{
				Name: childName,
				Path: displayWorkspacePath(childPath),
			}
			itemsByPath[childPath] = item
		}
		if hasMore || strings.TrimSpace(childTail) != "" {
			item.Type = workspaceEntryTypeDir
			item.HasChildren = true
			continue
		}

		switch entry.Type {
		case workspaceEntryTypeDir:
			item.Type = workspaceEntryTypeDir
		case workspaceEntryTypeFile, workspaceEntryTypeSymlink:
			item.Type = entry.Type
			item.Size = int64Ptr(entry.Size)
			item.MtimeUnixMs = int64Ptr(entry.MtimeUnixMs)
			mimeType := guessWorkspaceListMimeType(childName)
			item.MimeType = &mimeType
		}
	}

	items := make([]projectWorkspaceFileListItem, 0, len(itemsByPath))
	for _, item := range itemsByPath {
		if strings.TrimSpace(item.Type) == "" {
			item.Type = workspaceEntryTypeDir
		}
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			if items[i].Type == workspaceEntryTypeDir {
				return true
			}
			if items[j].Type == workspaceEntryTypeDir {
				return false
			}
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func guessWorkspaceListMimeType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext != "" {
		if guessed := mime.TypeByExtension(ext); strings.TrimSpace(guessed) != "" {
			return guessed
		}
	}
	return "application/octet-stream"
}

func int64Ptr(value int64) *int64 {
	copied := value
	return &copied
}
