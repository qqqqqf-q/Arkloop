package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const DebounceDelay = 300 * time.Millisecond

type FileWatcher struct {
	watcher    *fsnotify.Watcher
	manager    *Manager
	rootDir    string
	debouncer  map[string]*time.Timer
	debounceMu sync.Mutex
	logger     *slog.Logger
	done       chan struct{}
}

func NewFileWatcher(manager *Manager, rootDir string, logger *slog.Logger) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	fw := &FileWatcher{
		watcher:   w,
		manager:   manager,
		rootDir:   rootDir,
		debouncer: make(map[string]*time.Timer),
		logger:    logger,
		done:      make(chan struct{}),
	}

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if !info.IsDir() {
			return nil
		}
		if manager.ShouldIgnore(path) {
			return filepath.SkipDir
		}
		if err := w.Add(path); err != nil {
			logger.Warn("failed to watch directory", "path", path, "err", err)
		}
		return nil
	})
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("failed to walk directory tree: %w", err)
	}

	go fw.eventLoop()
	return fw, nil
}

func (fw *FileWatcher) eventLoop() {
	for {
		select {
		case <-fw.done:
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.handleEvent(event)
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			fw.logger.Error("fsnotify error", "err", err)
		}
	}
}

func (fw *FileWatcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	if fw.manager.ShouldIgnore(path) {
		return
	}

	// new directory: watch it recursively
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			filepath.Walk(path, func(sub string, si os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !si.IsDir() {
					return nil
				}
				if fw.manager.ShouldIgnore(sub) {
					return filepath.SkipDir
				}
				if err := fw.watcher.Add(sub); err != nil {
					fw.logger.Warn("failed to watch new directory", "path", sub, "err", err)
				}
				return nil
			})
			return
		}
	}

	// only handle regular files from here
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return
	}

	switch {
	case event.Has(fsnotify.Create):
		fw.debounce(path, func() {
			ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
			defer cancel()
			if err := fw.manager.NotifyFileCreated(ctx, path); err != nil {
				fw.logger.Debug("notify file created failed", "path", path, "err", err)
			}
		})

	case event.Has(fsnotify.Write):
		fw.debounce(path, func() {
			ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
			defer cancel()
			if err := fw.manager.NotifyFileChanged(ctx, path); err != nil {
				fw.logger.Debug("notify file changed failed", "path", path, "err", err)
			}
		})

	case event.Has(fsnotify.Remove), event.Has(fsnotify.Rename):
		fw.debounceMu.Lock()
		if t, ok := fw.debouncer[path]; ok {
			t.Stop()
			delete(fw.debouncer, path)
		}
		fw.debounceMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
		defer cancel()
		if err := fw.manager.NotifyFileDeleted(ctx, path); err != nil {
			fw.logger.Debug("notify file deleted failed", "path", path, "err", err)
		}
	}
}

func (fw *FileWatcher) debounce(path string, fn func()) {
	fw.debounceMu.Lock()
	defer fw.debounceMu.Unlock()

	if t, ok := fw.debouncer[path]; ok {
		t.Stop()
	}
	fw.debouncer[path] = time.AfterFunc(DebounceDelay, fn)
}

func (fw *FileWatcher) Stop() {
	close(fw.done)
	fw.watcher.Close()

	fw.debounceMu.Lock()
	for _, t := range fw.debouncer {
		t.Stop()
	}
	fw.debouncer = nil
	fw.debounceMu.Unlock()
}
