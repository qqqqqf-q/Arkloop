package environment

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/shared/objectstore"
)

const (
	ScopeProfile   = "profile"
	ScopeWorkspace = "workspace"

	debounceDelay = 300 * time.Millisecond
	flushTimeout  = 2 * time.Minute
)

type Carrier interface {
	ExportEnvironment(ctx context.Context, scope string) ([]byte, error)
	ImportEnvironment(ctx context.Context, scope string, archive []byte) error
}

type Store interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
}

type Binding struct {
	OrgID        string
	ProfileRef   string
	WorkspaceRef string
}

type Manager struct {
	store  Store
	logger *logging.JSONLogger

	mu       sync.Mutex
	sessions map[string]*trackedSession
}

type trackedSession struct {
	mu sync.Mutex

	carrier Carrier
	binding Binding
	dirty   bool
	version uint64
	timer   *time.Timer
}

func NewManager(store Store, logger *logging.JSONLogger) *Manager {
	return &Manager{
		store:    store,
		logger:   logger,
		sessions: make(map[string]*trackedSession),
	}
}

func (m *Manager) Prepare(ctx context.Context, sessionID string, carrier Carrier, binding Binding) error {
	if m == nil || m.store == nil || carrier == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	binding = normalizeBinding(binding)
	if binding.ProfileRef == "" || binding.WorkspaceRef == "" {
		return nil
	}

	entry := m.ensureSession(sessionID, carrier, binding)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.carrier = carrier
	entry.binding = binding
	if entry.dirty {
		return nil
	}
	if err := m.importScope(ctx, entry.carrier, ScopeProfile, profileKey(binding.ProfileRef)); err != nil {
		return err
	}
	if err := m.importScope(ctx, entry.carrier, ScopeWorkspace, workspaceKey(binding.WorkspaceRef)); err != nil {
		return err
	}
	return nil
}

func (m *Manager) MarkDirty(sessionID string) {
	if m == nil || m.store == nil {
		return
	}
	entry := m.lookupSession(strings.TrimSpace(sessionID))
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.dirty = true
	entry.version++
	if entry.timer != nil {
		entry.timer.Stop()
	}
	currentVersion := entry.version
	entry.timer = time.AfterFunc(debounceDelay, func() {
		m.flushInBackground(sessionID, currentVersion, false)
	})
}

func (m *Manager) FlushNow(ctx context.Context, sessionID string) error {
	return m.flush(ctx, strings.TrimSpace(sessionID), 0, true)
}

func (m *Manager) Drop(sessionID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[strings.TrimSpace(sessionID)]
	if !ok {
		return
	}
	entry.mu.Lock()
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.mu.Unlock()
	delete(m.sessions, strings.TrimSpace(sessionID))
}

func (m *Manager) flushInBackground(sessionID string, version uint64, force bool) {
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := m.flush(ctx, strings.TrimSpace(sessionID), version, force); err != nil && m.logger != nil {
		sid := strings.TrimSpace(sessionID)
		m.logger.Warn("environment flush failed", logging.LogFields{SessionID: &sid}, map[string]any{"error": err.Error()})
	}
}

func (m *Manager) flush(ctx context.Context, sessionID string, version uint64, force bool) error {
	if m == nil || m.store == nil || sessionID == "" {
		return nil
	}
	entry := m.lookupSession(sessionID)
	if entry == nil {
		return nil
	}

	entry.mu.Lock()
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	if !force && (!entry.dirty || entry.version != version) {
		entry.mu.Unlock()
		return nil
	}
	carrier := entry.carrier
	binding := entry.binding
	startVersion := entry.version
	entry.mu.Unlock()

	profileArchive, err := carrier.ExportEnvironment(ctx, ScopeProfile)
	if err != nil {
		return fmt.Errorf("export profile: %w", err)
	}
	if err := m.store.Put(ctx, profileKey(binding.ProfileRef), profileArchive); err != nil {
		return fmt.Errorf("put profile archive: %w", err)
	}
	workspaceArchive, err := carrier.ExportEnvironment(ctx, ScopeWorkspace)
	if err != nil {
		return fmt.Errorf("export workspace: %w", err)
	}
	if err := m.store.Put(ctx, workspaceKey(binding.WorkspaceRef), workspaceArchive); err != nil {
		return fmt.Errorf("put workspace archive: %w", err)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if force || entry.version == startVersion {
		entry.dirty = false
		return nil
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	currentVersion := entry.version
	entry.timer = time.AfterFunc(debounceDelay, func() {
		m.flushInBackground(sessionID, currentVersion, false)
	})
	return nil
}

func (m *Manager) importScope(ctx context.Context, carrier Carrier, scope string, key string) error {
	archive, err := m.store.Get(ctx, key)
	if err != nil {
		if objectstore.IsNotFound(err) {
			return nil
		}
		return err
	}
	return carrier.ImportEnvironment(ctx, scope, archive)
}

func (m *Manager) ensureSession(sessionID string, carrier Carrier, binding Binding) *trackedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.sessions[sessionID]; ok {
		return entry
	}
	entry := &trackedSession{carrier: carrier, binding: binding}
	m.sessions[sessionID] = entry
	return entry
}

func (m *Manager) lookupSession(sessionID string) *trackedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

func normalizeBinding(binding Binding) Binding {
	binding.OrgID = strings.TrimSpace(binding.OrgID)
	binding.ProfileRef = strings.TrimSpace(binding.ProfileRef)
	binding.WorkspaceRef = strings.TrimSpace(binding.WorkspaceRef)
	return binding
}

func profileKey(profileRef string) string {
	return "profiles/" + strings.TrimSpace(profileRef) + "/state.tar.zst"
}

func workspaceKey(workspaceRef string) string {
	return "workspaces/" + strings.TrimSpace(workspaceRef) + "/state.tar.zst"
}
