package personas

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

const watchInterval = 2 * time.Second

// WatchedRegistry 持有一个可热替换的 Registry，后台轮询文件 mtime 变化后自动重载。
type WatchedRegistry struct {
	ptr  atomic.Pointer[Registry]
	root string
}

func NewWatchedRegistry(root string, initial *Registry) *WatchedRegistry {
	w := &WatchedRegistry{root: root}
	w.ptr.Store(initial)
	return w
}

func (w *WatchedRegistry) Get() *Registry {
	return w.ptr.Load()
}

// Watch 启动后台轮询，ctx 取消时退出。
func (w *WatchedRegistry) Watch(ctx context.Context) {
	go func() {
		snapshot := w.collectMtimes()
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := w.collectMtimes()
				if !mtimesEqual(snapshot, current) {
					snapshot = current
					reg, err := LoadRegistry(w.root)
					if err != nil {
						slog.Warn("personas: reload failed", "err", err.Error())
						continue
					}
					w.ptr.Store(reg)
					slog.Info("personas: reloaded", "root", w.root)
				}
			}
		}
	}()
}

func (w *WatchedRegistry) collectMtimes() map[string]time.Time {
	out := map[string]time.Time{}
	entries, err := os.ReadDir(w.root)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, name := range watchedPersonaFiles(w.root, entry.Name()) {
			p := filepath.Join(w.root, entry.Name(), name)
			info, err := os.Stat(p)
			if err == nil {
				out[p] = info.ModTime()
			}
		}
	}
	return out
}

func watchedPersonaFiles(root string, personaDir string) []string {
	files := []string{"persona.yaml", "prompt.md"}
	yamlPath := filepath.Join(root, personaDir, "persona.yaml")
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		return append(files, "soul.md")
	}
	var obj map[string]any
	if err := yaml.Unmarshal(raw, &obj); err != nil {
		return append(files, "soul.md")
	}
	files = append(files, watchedSummarizePromptFiles(obj)...)
	rawSoulFile, ok := obj["soul_file"]
	if !ok {
		return append(files, "soul.md")
	}
	soulFile, ok := rawSoulFile.(string)
	if !ok {
		return files
	}
	soulFile = strings.TrimSpace(soulFile)
	if soulFile == "" || filepath.IsAbs(soulFile) {
		return files
	}
	cleaned := filepath.Clean(soulFile)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return files
	}
	return append(files, cleaned)
}

func watchedSummarizePromptFiles(obj map[string]any) []string {
	files := make([]string, 0, 2)
	for _, key := range []string{"title_summarize", "result_summarize"} {
		raw, ok := obj[key]
		if !ok || raw == nil {
			continue
		}
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rawFile, ok := block["prompt_file"]
		if !ok || rawFile == nil {
			continue
		}
		fileName, ok := rawFile.(string)
		if !ok {
			continue
		}
		fileName = strings.TrimSpace(fileName)
		if fileName == "" || filepath.IsAbs(fileName) {
			continue
		}
		cleaned := filepath.Clean(fileName)
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			continue
		}
		files = append(files, cleaned)
	}
	return files
}

func mtimesEqual(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
