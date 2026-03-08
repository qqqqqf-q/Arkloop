package environment

import (
	"encoding/base64"
	"fmt"
	"path"
	"sort"
	"strings"
)

const CurrentManifestVersion = 1

const (
	EntryTypeDir     = "dir"
	EntryTypeFile    = "file"
	EntryTypeSymlink = "symlink"

	legacyRevision = "legacy"
)

type Manifest struct {
	Version   int             `json:"version"`
	Scope     string          `json:"scope"`
	Ref       string          `json:"ref,omitempty"`
	Revision  string          `json:"revision,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
	Entries   []ManifestEntry `json:"entries,omitempty"`
}

type ManifestEntry struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	Mode       int64  `json:"mode,omitempty"`
	Size       int64  `json:"size,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	BlobKey    string `json:"blob_key,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

type FilePayload struct {
	Path   string `json:"path"`
	Data   string `json:"data,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Mode   int64  `json:"mode,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type PrepareOptions struct {
	WorkspaceMode WorkspaceHydrationMode
	WorkspaceCwd  string
}

type WorkspaceHydrationMode string

const (
	WorkspaceHydrationFull       WorkspaceHydrationMode = "full"
	WorkspaceHydrationCwdSubtree WorkspaceHydrationMode = "cwd_subtree"
)

func NormalizeManifest(manifest Manifest) Manifest {
	manifest.Version = CurrentManifestVersion
	manifest.Scope = strings.TrimSpace(manifest.Scope)
	manifest.Ref = strings.TrimSpace(manifest.Ref)
	manifest.Revision = strings.TrimSpace(manifest.Revision)
	manifest.UpdatedAt = strings.TrimSpace(manifest.UpdatedAt)

	unique := make(map[string]ManifestEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		normalized, ok := normalizeEntry(entry)
		if !ok {
			continue
		}
		unique[normalized.Path] = normalized
	}
	manifest.Entries = manifest.Entries[:0]
	for _, entry := range unique {
		manifest.Entries = append(manifest.Entries, entry)
	}
	sort.Slice(manifest.Entries, func(i, j int) bool {
		return manifest.Entries[i].Path < manifest.Entries[j].Path
	})
	return manifest
}

func CloneManifest(manifest Manifest) Manifest {
	clone := manifest
	if len(manifest.Entries) == 0 {
		clone.Entries = nil
		return clone
	}
	clone.Entries = append([]ManifestEntry(nil), manifest.Entries...)
	return clone
}

func EntryMap(entries []ManifestEntry) map[string]ManifestEntry {
	result := make(map[string]ManifestEntry, len(entries))
	for _, entry := range entries {
		result[entry.Path] = entry
	}
	return result
}

func normalizeEntry(entry ManifestEntry) (ManifestEntry, bool) {
	entry.Path = normalizeRelativePath(entry.Path)
	entry.Type = strings.TrimSpace(entry.Type)
	entry.SHA256 = strings.TrimSpace(entry.SHA256)
	entry.BlobKey = strings.TrimSpace(entry.BlobKey)
	entry.LinkTarget = strings.TrimSpace(entry.LinkTarget)
	if entry.Path == "" || entry.Type == "" {
		return ManifestEntry{}, false
	}
	if entry.Type != EntryTypeDir && entry.Type != EntryTypeFile && entry.Type != EntryTypeSymlink {
		return ManifestEntry{}, false
	}
	if entry.Type != EntryTypeFile {
		entry.Size = 0
		entry.SHA256 = ""
		entry.BlobKey = ""
	}
	if entry.Type != EntryTypeSymlink {
		entry.LinkTarget = ""
	}
	return entry, true
}

func normalizeRelativePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	cleaned := path.Clean(strings.TrimPrefix(strings.ReplaceAll(trimmed, "\\", "/"), "/"))
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return ""
	}
	return cleaned
}

func EncodeFilePayload(path string, data []byte, entry ManifestEntry) FilePayload {
	return FilePayload{
		Path:   normalizeRelativePath(path),
		Data:   base64.StdEncoding.EncodeToString(data),
		Size:   entry.Size,
		Mode:   entry.Mode,
		SHA256: entry.SHA256,
	}
}

func DecodeFilePayload(payload FilePayload) ([]byte, error) {
	if payload.Path = normalizeRelativePath(payload.Path); payload.Path == "" {
		return nil, fmt.Errorf("file payload path is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Data))
	if err != nil {
		return nil, fmt.Errorf("decode file payload %s: %w", payload.Path, err)
	}
	return decoded, nil
}
