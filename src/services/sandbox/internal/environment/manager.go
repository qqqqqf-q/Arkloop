package environment

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/shared/objectstore"
)

const (
	ScopeProfile   = "profile"
	ScopeWorkspace = "workspace"

	debounceDelay       = 2 * time.Second
	flushTimeout        = 2 * time.Minute
	maxDirtyAge         = 15 * time.Second
	forceBytesThreshold = 16 << 20
	forceCountThreshold = 512
	profileRootPath     = "/home/arkloop"
	workspaceRootPath   = "/workspace"
)

type Carrier interface {
	BuildEnvironmentManifest(ctx context.Context, scope string, subtrees []string) (Manifest, error)
	CollectEnvironmentFiles(ctx context.Context, scope string, paths []string) ([]FilePayload, error)
	ApplyEnvironment(ctx context.Context, scope string, manifest Manifest, files []FilePayload, reset bool) error
	ExportEnvironment(ctx context.Context, scope string) ([]byte, error)
	ImportEnvironment(ctx context.Context, scope string, archive []byte) error
}

type Binding struct {
	OrgID        string
	ProfileRef   string
	WorkspaceRef string
}

type Manager struct {
	store    objectstore.BlobStore
	registry RegistryWriter
	logger   *logging.JSONLogger

	mu       sync.Mutex
	sessions map[string]*trackedSession
}

type trackedSession struct {
	mu sync.Mutex

	carrier Carrier
	binding Binding
	scopes  map[string]*trackedScope
}

type trackedScope struct {
	dirtySubtrees      map[string]struct{}
	dirtyCount         int
	pendingBytes       int64
	firstDirtyAt       time.Time
	lastDirtyAt        time.Time
	hydratedRevision   string
	version            uint64
	running            bool
	runDone            chan struct{}
	timer              *time.Timer
	needsFullReconcile bool
}

func NewManager(store objectstore.BlobStore, registry RegistryWriter, logger *logging.JSONLogger) *Manager {
	if registry == nil {
		registry = NewNoopRegistryWriter()
	}
	return &Manager{
		store:    store,
		registry: registry,
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
	if verifier, ok := carrier.(interface{ EnsureEnvironmentProtocol(context.Context) error }); ok {
		if err := verifier.EnsureEnvironmentProtocol(ctx); err != nil {
			return err
		}
	}
	if err := m.registry.EnsureProfileRegistry(ctx, binding.OrgID, binding.ProfileRef); err != nil {
		return err
	}
	if err := m.registry.EnsureWorkspaceRegistry(ctx, binding.OrgID, binding.WorkspaceRef); err != nil {
		return err
	}

	entry := m.ensureSession(sessionID, carrier, binding)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.carrier = carrier
	entry.binding = binding
	if entry.hasDirtyLocked() {
		return nil
	}
	if err := m.prepareScope(ctx, entry.carrier, entry.scopeLocked(ScopeProfile), ScopeProfile, binding.ProfileRef); err != nil {
		return err
	}
	if err := m.prepareScope(ctx, entry.carrier, entry.scopeLocked(ScopeWorkspace), ScopeWorkspace, binding.WorkspaceRef); err != nil {
		return err
	}
	return nil
}

func (m *Manager) MarkDirty(sessionID, cwd string) {
	if m == nil || m.store == nil {
		return
	}
	entry := m.lookupSession(strings.TrimSpace(sessionID))
	if entry == nil {
		return
	}
	updates := dirtyScopesForCwd(cwd)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	for scope, subtree := range updates {
		state := entry.scopeLocked(scope)
		state.markDirty(subtree)
		m.scheduleScopeLocked(strings.TrimSpace(sessionID), scope, state)
	}
}

func (m *Manager) FlushNow(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	entry := m.lookupSession(sessionID)
	if entry == nil {
		return nil
	}
	entry.mu.Lock()
	for scopeName, state := range entry.scopes {
		if !state.hasDirty() {
			continue
		}
		state.needsFullReconcile = true
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		if !state.hasRootDirty() {
			state.dirtySubtrees[""] = struct{}{}
			state.dirtyCount = len(state.dirtySubtrees)
		}
		state.version++
		_ = scopeName
	}
	entry.mu.Unlock()

	if err := m.flushScope(ctx, sessionID, ScopeProfile, 0, true); err != nil {
		return err
	}
	if err := m.flushScope(ctx, sessionID, ScopeWorkspace, 0, true); err != nil {
		return err
	}
	return nil
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
	for _, state := range entry.scopes {
		if state.timer != nil {
			state.timer.Stop()
		}
	}
	entry.mu.Unlock()
	delete(m.sessions, strings.TrimSpace(sessionID))
}

func (m *Manager) flushScopeInBackground(sessionID, scope string, version uint64, force bool) {
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := m.flushScope(ctx, strings.TrimSpace(sessionID), strings.TrimSpace(scope), version, force); err != nil && m.logger != nil {
		sid := strings.TrimSpace(sessionID)
		m.logger.Warn("environment flush failed", logging.LogFields{SessionID: &sid}, map[string]any{"scope": scope, "error": err.Error()})
	}
}

func (m *Manager) flushScope(ctx context.Context, sessionID, scope string, version uint64, force bool) error {
	for {
		if m == nil || m.store == nil || sessionID == "" {
			return nil
		}
		entry := m.lookupSession(sessionID)
		if entry == nil {
			return nil
		}

		entry.mu.Lock()
		state := entry.scopeLocked(scope)
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		if force {
			state.needsFullReconcile = true
		}
		if state.running {
			done := state.runDone
			entry.mu.Unlock()
			if force && done != nil {
				select {
				case <-done:
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		if !state.hasDirty() {
			entry.mu.Unlock()
			return nil
		}
		if !force && version != 0 && state.version != version {
			entry.mu.Unlock()
			return nil
		}
		carrier := entry.carrier
		binding := entry.binding
		startVersion := state.version
		dirtySubtrees := state.sortedDirtySubtrees()
		fullReconcile := force || state.needsFullReconcile || state.pendingBytes >= forceBytesThreshold || state.dirtyCount >= forceCountThreshold || (!state.firstDirtyAt.IsZero() && time.Since(state.firstDirtyAt) >= maxDirtyAge) || len(dirtySubtrees) == 0
		state.running = true
		state.runDone = make(chan struct{})
		state.needsFullReconcile = false
		entry.mu.Unlock()

		revision, err := m.runScopeFlush(ctx, carrier, binding, scope, dirtySubtrees, fullReconcile)

		entry = m.lookupSession(sessionID)
		if entry == nil {
			return err
		}
		entry.mu.Lock()
		state = entry.scopeLocked(scope)
		done := state.runDone
		state.running = false
		state.runDone = nil
		if done != nil {
			close(done)
		}
		if err == nil {
			state.hydratedRevision = strings.TrimSpace(revision)
			if state.version == startVersion {
				state.resetDirty()
				entry.mu.Unlock()
				return nil
			}
			m.scheduleScopeLocked(sessionID, scope, state)
			entry.mu.Unlock()
			return nil
		}
		state.needsFullReconcile = true
		m.scheduleScopeLocked(sessionID, scope, state)
		entry.mu.Unlock()
		return err
	}
}

func (m *Manager) runScopeFlush(ctx context.Context, carrier Carrier, binding Binding, scope string, dirtySubtrees []string, fullReconcile bool) (_ string, err error) {
	ref := binding.refForScope(scope)
	if ref == "" {
		return "", nil
	}
	if err = m.registry.MarkFlushPending(ctx, scope, ref); err != nil {
		return "", err
	}
	if err = m.registry.MarkFlushRunning(ctx, scope, ref); err != nil {
		_ = m.registry.MarkFlushFailed(ctx, scope, ref, time.Now().UTC())
		return "", err
	}
	now := time.Now().UTC()
	defer func() {
		if err != nil {
			_ = m.registry.MarkFlushFailed(ctx, scope, ref, now)
		}
	}()

	baseRevision, err := m.registry.GetLatestManifestRevision(ctx, scope, ref)
	if err != nil {
		return "", err
	}

	var previous Manifest
	var hasPrevious bool
	if strings.TrimSpace(baseRevision) != "" {
		loaded, loadErr := loadManifest(ctx, m.store, scope, ref, baseRevision)
		if loadErr != nil {
			if !objectstore.IsNotFound(loadErr) {
				return "", loadErr
			}
			fullReconcile = true
		} else {
			previous = *loaded
			hasPrevious = true
		}
	}

	var scanned Manifest
	if fullReconcile || !hasPrevious {
		scanned, err = carrier.BuildEnvironmentManifest(ctx, scope, nil)
	} else {
		scanned, err = carrier.BuildEnvironmentManifest(ctx, scope, dirtySubtrees)
	}
	if err != nil {
		return "", err
	}
	revision := nextManifestRevision(now)
	nextManifest := mergeManifest(scope, ref, revision, baseRevision, previous, scanned, fullReconcile || !hasPrevious, dirtySubtrees)

	changedPaths := changedRegularFilePaths(previous, nextManifest)
	if len(changedPaths) > 0 {
		files, collectErr := carrier.CollectEnvironmentFiles(ctx, scope, changedPaths)
		if collectErr != nil {
			return "", collectErr
		}
		payloads := make(map[string]FilePayload, len(files))
		for _, payload := range files {
			payloads[normalizeRelativePath(payload.Path)] = payload
		}
		for _, path := range changedPaths {
			entry, ok := EntryMap(nextManifest.Entries)[path]
			if !ok || entry.Type != EntryTypeFile {
				continue
			}
			payload, ok := payloads[path]
			if !ok {
				return "", fmt.Errorf("missing file payload: %s", path)
			}
			data, decodeErr := DecodeFilePayload(payload)
			if decodeErr != nil {
				return "", decodeErr
			}
			if err := putBlobIfMissing(ctx, m.store, blobKey(scope, ref, entry.SHA256), data); err != nil {
				return "", err
			}
		}
	}

	if err := saveManifest(ctx, m.store, nextManifest); err != nil {
		return "", err
	}
	if err := saveLatestPointer(ctx, m.store, LatestPointer{
		Version:   CurrentManifestVersion,
		Scope:     scope,
		Ref:       ref,
		Revision:  revision,
		UpdatedAt: now.Format(time.RFC3339Nano),
	}); err != nil {
		return "", err
	}
	if err := m.registry.MarkFlushSucceeded(ctx, scope, ref, revision, now); err != nil {
		return "", err
	}
	return revision, nil
}

func (m *Manager) prepareScope(ctx context.Context, carrier Carrier, state *trackedScope, scope string, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	revision, err := m.registry.GetLatestManifestRevision(ctx, scope, ref)
	if err != nil {
		return err
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		pointer, pointerErr := loadLatestPointer(ctx, m.store, scope, ref)
		if pointerErr == nil {
			revision = strings.TrimSpace(pointer.Revision)
		} else if !objectstore.IsNotFound(pointerErr) {
			return pointerErr
		}
	}
	if revision == "" {
		if err := m.importScope(ctx, carrier, scope, ref); err != nil {
			return err
		}
		if state != nil {
			state.hydratedRevision = ""
		}
		return nil
	}
	if state != nil && !state.hasDirty() && state.hydratedRevision == revision {
		return nil
	}
	if err := hydrateScope(ctx, m.store, carrier, scope, ref, revision); err != nil {
		return err
	}
	if state != nil {
		state.hydratedRevision = revision
	}
	return nil
}

func (m *Manager) importScope(ctx context.Context, carrier Carrier, scope string, ref string) error {
	ref = strings.TrimSpace(ref)
	archive, err := m.store.Get(ctx, legacyArchiveKey(scope, ref))
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
	entry := &trackedSession{
		carrier: carrier,
		binding: binding,
		scopes: map[string]*trackedScope{
			ScopeProfile:   newTrackedScope(),
			ScopeWorkspace: newTrackedScope(),
		},
	}
	m.sessions[sessionID] = entry
	return entry
}

func (m *Manager) lookupSession(sessionID string) *trackedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

func (m *Manager) scheduleScopeLocked(sessionID, scope string, state *trackedScope) {
	if state == nil {
		return
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	version := state.version
	delay := debounceDelay
	if state.pendingBytes >= forceBytesThreshold || state.dirtyCount >= forceCountThreshold || (!state.firstDirtyAt.IsZero() && time.Since(state.firstDirtyAt) >= maxDirtyAge) {
		delay = 0
	}
	state.timer = time.AfterFunc(delay, func() {
		m.flushScopeInBackground(sessionID, scope, version, false)
	})
}

func normalizeBinding(binding Binding) Binding {
	binding.OrgID = strings.TrimSpace(binding.OrgID)
	binding.ProfileRef = strings.TrimSpace(binding.ProfileRef)
	binding.WorkspaceRef = strings.TrimSpace(binding.WorkspaceRef)
	return binding
}

func (b Binding) refForScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return b.ProfileRef
	case ScopeWorkspace:
		return b.WorkspaceRef
	default:
		return ""
	}
}

func (s *trackedSession) hasDirtyLocked() bool {
	for _, scope := range s.scopes {
		if scope.hasDirty() {
			return true
		}
	}
	return false
}

func (s *trackedSession) scopeLocked(scope string) *trackedScope {
	state, ok := s.scopes[strings.TrimSpace(scope)]
	if ok {
		return state
	}
	state = newTrackedScope()
	s.scopes[strings.TrimSpace(scope)] = state
	return state
}

func newTrackedScope() *trackedScope {
	return &trackedScope{dirtySubtrees: make(map[string]struct{})}
}

func (s *trackedScope) hasDirty() bool {
	return len(s.dirtySubtrees) > 0
}

func (s *trackedScope) hasRootDirty() bool {
	_, ok := s.dirtySubtrees[""]
	return ok
}

func (s *trackedScope) markDirty(subtree string) {
	now := time.Now().UTC()
	if s.firstDirtyAt.IsZero() {
		s.firstDirtyAt = now
	}
	s.lastDirtyAt = now
	s.version++
	addDirtySubtree(s.dirtySubtrees, normalizeRelativePath(subtree))
	s.dirtyCount = len(s.dirtySubtrees)
}

func (s *trackedScope) resetDirty() {
	s.dirtySubtrees = make(map[string]struct{})
	s.dirtyCount = 0
	s.pendingBytes = 0
	s.firstDirtyAt = time.Time{}
	s.lastDirtyAt = time.Time{}
	s.needsFullReconcile = false
}

func (s *trackedScope) sortedDirtySubtrees() []string {
	items := make([]string, 0, len(s.dirtySubtrees))
	for subtree := range s.dirtySubtrees {
		items = append(items, subtree)
	}
	sort.Strings(items)
	return items
}

func addDirtySubtree(target map[string]struct{}, subtree string) {
	if len(target) == 0 {
		target[subtree] = struct{}{}
		return
	}
	if subtree == "" {
		for key := range target {
			delete(target, key)
		}
		target[""] = struct{}{}
		return
	}
	if _, ok := target[""]; ok {
		return
	}
	for existing := range target {
		if existing == subtree || strings.HasPrefix(subtree, existing+"/") {
			return
		}
	}
	for existing := range target {
		if strings.HasPrefix(existing, subtree+"/") {
			delete(target, existing)
		}
	}
	target[subtree] = struct{}{}
}

func dirtyScopesForCwd(cwd string) map[string]string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return map[string]string{ScopeProfile: "", ScopeWorkspace: ""}
	}
	if subtree, ok := subtreeWithinRoot(cwd, workspaceRootPath); ok {
		return map[string]string{ScopeWorkspace: subtree}
	}
	if subtree, ok := subtreeWithinRoot(cwd, profileRootPath); ok {
		return map[string]string{ScopeProfile: subtree}
	}
	return map[string]string{ScopeProfile: "", ScopeWorkspace: ""}
}

func subtreeWithinRoot(cwd, root string) (string, bool) {
	if cwd == root || cwd == root+"/" {
		return "", true
	}
	if !strings.HasPrefix(cwd, root+"/") {
		return "", false
	}
	return normalizeRelativePath(strings.TrimPrefix(cwd, root+"/")), true
}

func nextManifestRevision(now time.Time) string {
	return fmt.Sprintf("%d", now.UTC().UnixNano())
}

func mergeManifest(scope, ref, revision, baseRevision string, previous Manifest, scanned Manifest, full bool, dirtySubtrees []string) Manifest {
	if full {
		scanned.Scope = scope
		scanned.Ref = ref
		scanned.Revision = revision
		scanned.BaseRevision = strings.TrimSpace(baseRevision)
		scanned.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return NormalizeManifest(scanned)
	}
	entries := make([]ManifestEntry, 0, len(previous.Entries)+len(scanned.Entries))
	for _, entry := range previous.Entries {
		if pathMatchesAnySubtree(entry.Path, dirtySubtrees) {
			continue
		}
		entries = append(entries, entry)
	}
	entries = append(entries, scanned.Entries...)
	return NormalizeManifest(Manifest{
		Version:      CurrentManifestVersion,
		Scope:        scope,
		Ref:          ref,
		Revision:     revision,
		BaseRevision: strings.TrimSpace(baseRevision),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Entries:      entries,
	})
}

func pathMatchesAnySubtree(path string, subtrees []string) bool {
	for _, subtree := range subtrees {
		if subtree == "" || path == subtree || strings.HasPrefix(path, subtree+"/") {
			return true
		}
	}
	return false
}

func changedRegularFilePaths(previous Manifest, next Manifest) []string {
	prevMap := EntryMap(previous.Entries)
	changed := make([]string, 0)
	for _, entry := range next.Entries {
		if entry.Type != EntryTypeFile {
			continue
		}
		previousEntry, ok := prevMap[entry.Path]
		if !ok || previousEntry.Type != EntryTypeFile || previousEntry.SHA256 != entry.SHA256 {
			changed = append(changed, entry.Path)
		}
	}
	sort.Strings(changed)
	return changed
}

func profileKey(profileRef string) string {
	return "profiles/" + strings.TrimSpace(profileRef) + "/state.tar.zst"
}

func workspaceKey(workspaceRef string) string {
	return "workspaces/" + strings.TrimSpace(workspaceRef) + "/state.tar.zst"
}
