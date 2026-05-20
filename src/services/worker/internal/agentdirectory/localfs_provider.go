package agentdirectory

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// LocalFSProvider 从本地文件系统读取 AWD 文件，用于 Desktop localshell。
type LocalFSProvider struct {
	homeDirFunc func() string
	seedOnce    sync.Once
}

func NewLocalFSProvider(homeDirFunc func() string) *LocalFSProvider {
	return &LocalFSProvider{homeDirFunc: homeDirFunc}
}

func (p *LocalFSProvider) Load(_ context.Context, _ string) (*Content, error) {
	base := p.homeDirFunc()
	if err := os.MkdirAll(base, 0o755); err != nil {
		slog.Error("agentdirectory: failed to create workspace directory", "path", base, "error", err)
	}

	p.seedOnce.Do(func() {
		if _, err := SeedWorkspace(base); err != nil {
			slog.Error("agentdirectory: seeding failed", "path", base, "error", err)
		}
	})

	content := &Content{WorkDirPath: base}

	files := map[string]*string{
		"SOUL.md":      &content.Soul,
		"AGENTS.md":    &content.Instructions,
		"MEMORY.md":    &content.Memory,
		"USER.md":      &content.User,
		"BOOTSTRAP.md": &content.Bootstrap,
		"IDENTITY.md":  &content.Identity,
		"TOOLS.md":     &content.Tools,
		"HEARTBEAT.md": &content.Heartbeat,
	}
	for name, ptr := range files {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			continue
		}
		*ptr = string(data)
	}
	content.ExtraFiles = loadExtraMarkdownFiles(base, files)
	content.BootstrapPending = content.Bootstrap != ""
	content.DailyMemoryFiles = loadDailyMemoryFiles(base)
	return content, nil
}

// loadDailyMemoryFiles 从 memory/ 子目录读取最近的日志文件。
// 仅读取 memory/ 根目录下的 .md 文件，不递归子目录。
func loadDailyMemoryFiles(base string) []FileContent {
	memoryDir := filepath.Join(base, "memory")
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		return nil
	}
	var files []FileContent
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(memoryDir, name))
		if err != nil {
			continue
		}
		files = append(files, FileContent{Path: "memory/" + name, Content: string(data)})
	}
	// 按文件名倒序，最近的日期排前面
	sort.Slice(files, func(i, j int) bool { return files[i].Path > files[j].Path })
	return files
}

func loadExtraMarkdownFiles(base string, canonical map[string]*string) []FileContent {
	extras := []FileContent{}
	entries, err := os.ReadDir(base)
	if err != nil {
		return extras
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			continue
		}
		if _, ok := canonical[name]; ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			continue
		}
		extras = append(extras, FileContent{Path: name, Content: string(data)})
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i].Path < extras[j].Path })
	return extras
}
