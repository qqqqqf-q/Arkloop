package personas

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
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
		for _, name := range []string{"persona.yaml", "prompt.md"} {
			p := filepath.Join(w.root, entry.Name(), name)
			info, err := os.Stat(p)
			if err == nil {
				out[p] = info.ModTime()
			}
		}
	}
	return out
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
