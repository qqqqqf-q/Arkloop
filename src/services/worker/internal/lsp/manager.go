package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ManagerStopTimeout = 10 * time.Second
)

var ignoredDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"__pycache__":  true,
	".cache":       true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".next":        true,
}

var allowedDotDirs = map[string]bool{
	".github": true,
	".vscode": true,
	".config": true,
}

type Manager struct {
	mu          sync.RWMutex
	servers     map[string]*ServerInstance // config name -> instance
	extMap      map[string]string          // ".go" -> config name
	fileTracker map[string]string          // abs path -> config name
	config      *Config
	rootDir     string
	diagReg     *DiagnosticRegistry
	logger      *slog.Logger
	watcher     *FileWatcher
	started     atomic.Bool
}

func NewManager(config *Config, rootDir string, logger *slog.Logger) *Manager {
	extMap := make(map[string]string)
	for name, sc := range config.Servers {
		for _, ext := range sc.Extensions {
			extMap[strings.ToLower(ext)] = name
		}
	}

	return &Manager{
		servers:     make(map[string]*ServerInstance),
		extMap:      extMap,
		fileTracker: make(map[string]string),
		config:      config,
		rootDir:     rootDir,
		diagReg:     NewDiagnosticRegistry(logger),
		logger:      logger,
	}
}

// SetRootDir updates the workspace root directory.
// If the directory actually changed and the manager is running,
// all servers and the file watcher are stopped and will restart lazily.
func (m *Manager) SetRootDir(dir string) {
	m.mu.Lock()
	old := m.rootDir
	m.rootDir = dir
	m.mu.Unlock()

	if old == dir {
		return
	}
	if !m.started.Load() {
		return
	}

	m.logger.Info("lsp root changed, resetting servers", "old", old, "new", dir)

	// stop watcher under lock to avoid race with Start()
	m.mu.Lock()
	w := m.watcher
	m.watcher = nil
	m.mu.Unlock()
	if w != nil {
		w.Stop()
	}

	// stop all servers with timeout
	ctx, cancel := context.WithTimeout(context.Background(), ManagerStopTimeout)
	defer cancel()

	m.mu.Lock()
	servers := make(map[string]*ServerInstance, len(m.servers))
	for k, v := range m.servers {
		servers[k] = v
	}
	m.servers = make(map[string]*ServerInstance)
	m.fileTracker = make(map[string]string)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for name, si := range servers {
		wg.Add(1)
		go func(name string, si *ServerInstance) {
			defer wg.Done()
			if err := si.Stop(ctx); err != nil {
				m.logger.Error("failed to stop lsp server during root change", "server", name, "err", err)
			}
		}(name, si)
	}
	wg.Wait()

	// restart watcher with new root
	fw, err := NewFileWatcher(m, dir, m.logger)
	if err != nil {
		m.logger.Error("failed to restart file watcher", "err", err)
		return
	}
	m.mu.Lock()
	m.watcher = fw
	m.mu.Unlock()
}

func (m *Manager) RootDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rootDir
}

func (m *Manager) Start(_ context.Context) error {
	m.started.Store(true)

	root := m.RootDir()
	if root == "" {
		// rootDir not yet known; watcher starts when SetRootDir is called
		return nil
	}

	fw, err := NewFileWatcher(m, root, m.logger)
	if err != nil {
		m.logger.Error("failed to start file watcher", "err", err)
		// non-fatal: LSP still works without file watching
		return nil
	}
	m.watcher = fw
	return nil
}

func (m *Manager) ServerForFile(path string) (*ServerInstance, error) {
	ext := strings.ToLower(filepath.Ext(path))
	m.mu.RLock()
	name, ok := m.extMap[ext]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no LSP server configured for extension %q", ext)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	si, exists := m.servers[name]
	if !exists {
		sc := m.config.Servers[name]
		rootURI := PathToURI(m.rootDir)
		si = NewServerInstance(sc, rootURI, m.logger, m.diagReg)

		// subscribe to diagnostics
		si.onCrash = func() {
			m.logger.Warn("lsp server crashed", "server", name)
		}

		m.servers[name] = si
	}

	m.fileTracker[path] = name
	return si, nil
}

func (m *Manager) NotifyFileChanged(ctx context.Context, path string) error {
	m.mu.RLock()
	name, ok := m.fileTracker[path]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	m.mu.RLock()
	si, exists := m.servers[name]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	return si.Execute(ctx, func(c *Client) error {
		if err := c.DidChange(ctx, path); err != nil {
			return err
		}
		return c.DidSave(ctx, path)
	})
}

func (m *Manager) NotifyFileCreated(ctx context.Context, path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	m.mu.RLock()
	name, ok := m.extMap[ext]
	if !ok {
		m.mu.RUnlock()
		return nil
	}
	si, exists := m.servers[name]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	m.mu.Lock()
	m.fileTracker[path] = name
	m.mu.Unlock()

	return si.Execute(ctx, func(c *Client) error {
		return c.DidOpen(ctx, path)
	})
}

func (m *Manager) NotifyFileDeleted(ctx context.Context, path string) error {
	m.mu.Lock()
	name, ok := m.fileTracker[path]
	if ok {
		delete(m.fileTracker, path)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}

	m.mu.RLock()
	si, exists := m.servers[name]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	return si.Execute(ctx, func(c *Client) error {
		return c.DidClose(ctx, path)
	})
}

func (m *Manager) DiagRegistry() *DiagnosticRegistry {
	return m.diagReg
}

func (m *Manager) ExecuteOnFile(ctx context.Context, path string, fn func(*Client) error) error {
	si, err := m.ServerForFile(path)
	if err != nil {
		return err
	}
	return si.Execute(ctx, fn)
}

func (m *Manager) Stop(ctx context.Context) error {
	m.started.Store(false)

	// read and clear watcher under lock
	m.mu.Lock()
	w := m.watcher
	m.watcher = nil
	m.mu.Unlock()
	if w != nil {
		w.Stop()
	}

	ctx, cancel := context.WithTimeout(ctx, ManagerStopTimeout)
	defer cancel()

	m.mu.Lock()
	servers := make(map[string]*ServerInstance, len(m.servers))
	for k, v := range m.servers {
		servers[k] = v
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for name, si := range servers {
		wg.Add(1)
		go func(name string, si *ServerInstance) {
			defer wg.Done()
			if err := si.Stop(ctx); err != nil {
				m.logger.Error("failed to stop lsp server", "server", name, "err", err)
			}
		}(name, si)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		m.logger.Error("timeout stopping lsp servers")
	}

	m.mu.Lock()
	m.servers = make(map[string]*ServerInstance)
	m.fileTracker = make(map[string]string)
	m.mu.Unlock()

	return nil
}

func (m *Manager) ShouldIgnore(path string) bool {
	rootDir := m.RootDir()
	rel, err := filepath.Rel(rootDir, path)
	if err != nil {
		return false
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		if ignoredDirs[part] {
			return true
		}
		if strings.HasPrefix(part, ".") && len(part) > 1 && !allowedDotDirs[part] {
			return true
		}
	}
	return false
}

func (m *Manager) GetDiagnostics() string {
	return m.diagReg.FormatForPrompt(m.RootDir())
}
