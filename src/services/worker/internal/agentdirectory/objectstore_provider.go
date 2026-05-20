package agentdirectory

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
)

type manifestEntry struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	SHA256  string `json:"sha256"`
	Deleted bool   `json:"deleted"`
}

type manifest struct {
	Entries []manifestEntry `json:"entries"`
}

// BlobStoreReader 是 objectstore.BlobStore 的读取子集。
type BlobStoreReader interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// ObjectStoreProvider 从 object store 读取 profile scope AWD 文件。
// 空 manifest 时 fallback 到 embed seed 模板。
type ObjectStoreProvider struct {
	store       BlobStoreReader
	getRevision func(ctx context.Context, profileRef string) (string, error)
	workDirPath string
}

func NewObjectStoreProvider(
	store BlobStoreReader,
	getRevision func(ctx context.Context, profileRef string) (string, error),
	workDirPath string,
) *ObjectStoreProvider {
	return &ObjectStoreProvider{store: store, getRevision: getRevision, workDirPath: workDirPath}
}

func (p *ObjectStoreProvider) Load(ctx context.Context, profileRef string) (*Content, error) {
	if p.store == nil {
		return nil, nil
	}

	rev, err := p.getRevision(ctx, profileRef)
	if err != nil {
		return nil, fmt.Errorf("agentdirectory: get revision: %w", err)
	}
	if rev == "" {
		// Profile 还没有 workspace 文件，fallback 到 seed 模板
		return seedFallbackContent(p.workDirPath), nil
	}

	manifestKey := "profiles/" + profileRef + "/manifests/" + rev + ".json"
	manifestData, err := p.store.Get(ctx, manifestKey)
	if err != nil {
		return nil, fmt.Errorf("agentdirectory: get manifest: %w", err)
	}

	var m manifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return nil, fmt.Errorf("agentdirectory: unmarshal manifest: %w", err)
	}

	content := &Content{WorkDirPath: p.workDirPath}
	fieldMap := map[string]*string{
		"SOUL.md":      &content.Soul,
		"AGENTS.md":    &content.Instructions,
		"MEMORY.md":    &content.Memory,
		"USER.md":      &content.User,
		"BOOTSTRAP.md": &content.Bootstrap,
		"IDENTITY.md":  &content.Identity,
		"TOOLS.md":     &content.Tools,
		"HEARTBEAT.md": &content.Heartbeat,
	}

	for _, entry := range m.Entries {
		if entry.Deleted || entry.Type != "file" {
			continue
		}
		ptr, ok := fieldMap[entry.Path]
		if ok {
			blobKey := "profiles/" + profileRef + "/blobs/" + entry.SHA256
			data, err := p.store.Get(ctx, blobKey)
			if err != nil {
				continue
			}
			*ptr = string(data)
			continue
		}
		// memory/ 子目录下的 .md 文件 → DailyMemoryFiles
		if strings.HasPrefix(entry.Path, "memory/") && strings.EqualFold(pathExt(entry.Path), ".md") {
			blobKey := "profiles/" + profileRef + "/blobs/" + entry.SHA256
			data, err := p.store.Get(ctx, blobKey)
			if err != nil {
				continue
			}
			content.DailyMemoryFiles = append(content.DailyMemoryFiles, FileContent{Path: entry.Path, Content: string(data)})
			continue
		}
		if strings.Contains(entry.Path, "/") || !strings.EqualFold(pathExt(entry.Path), ".md") {
			continue
		}
		blobKey := "profiles/" + profileRef + "/blobs/" + entry.SHA256
		data, err := p.store.Get(ctx, blobKey)
		if err != nil {
			continue
		}
		content.ExtraFiles = append(content.ExtraFiles, FileContent{Path: entry.Path, Content: string(data)})
	}
	sort.Slice(content.ExtraFiles, func(i, j int) bool { return content.ExtraFiles[i].Path < content.ExtraFiles[j].Path })
	sort.Slice(content.DailyMemoryFiles, func(i, j int) bool { return content.DailyMemoryFiles[i].Path > content.DailyMemoryFiles[j].Path })

	content.BootstrapPending = content.Bootstrap != ""
	return content, nil
}

// seedFallbackContent 在 object store 无文件时返回 embed seed 模板作为 fallback。
func seedFallbackContent(workDirPath string) *Content {
	c := &Content{WorkDirPath: workDirPath}
	fieldMap := map[string]*string{
		"AGENTS.md":    &c.Instructions,
		"SOUL.md":      &c.Soul,
		"IDENTITY.md":  &c.Identity,
		"USER.md":      &c.User,
		"TOOLS.md":     &c.Tools,
		"BOOTSTRAP.md": &c.Bootstrap,
		"HEARTBEAT.md": &c.Heartbeat,
	}
	for name, ptr := range fieldMap {
		data, err := fs.ReadFile(templateFS, filepath.Join("templates", name))
		if err != nil {
			slog.Error("agentdirectory: missing seed template", "name", name, "error", err)
			continue
		}
		*ptr = string(data)
	}
	// MEMORY.md 无 template，agent 自行创建
	c.BootstrapPending = c.Bootstrap != ""
	return c
}

func pathExt(value string) string {
	index := strings.LastIndex(value, ".")
	if index < 0 {
		return ""
	}
	return value[index:]
}
