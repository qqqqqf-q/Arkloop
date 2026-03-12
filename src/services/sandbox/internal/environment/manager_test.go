package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/shared/objectstore"
)

type memoryStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{data: make(map[string][]byte)}
}

func (s *memoryStore) Put(_ context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = append([]byte(nil), data...)
	return nil
}

func (s *memoryStore) PutIfAbsent(_ context.Context, key string, data []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; ok {
		return false, nil
	}
	s.data[key] = append([]byte(nil), data...)
	return true, nil
}

func (s *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), value...), nil
}

func (s *memoryStore) Head(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	if !ok {
		return objectstore.ObjectInfo{}, os.ErrNotExist
	}
	return objectstore.ObjectInfo{Key: key, Size: int64(len(value))}, nil
}

func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *memoryStore) ListPrefix(_ context.Context, prefix string) ([]objectstore.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]objectstore.ObjectInfo, 0)
	for key, value := range s.data {
		if prefix == "" || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			items = append(items, objectstore.ObjectInfo{Key: key, Size: int64(len(value))})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func (s *memoryStore) WriteJSONAtomic(ctx context.Context, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.Put(ctx, key, payload)
}

type fakeRegistryWriter struct {
	mu          sync.Mutex
	latest      map[string]string
	ensured     []string
	transitions []string
	bindings    map[string]string
}

func newFakeRegistryWriter() *fakeRegistryWriter {
	return &fakeRegistryWriter{latest: make(map[string]string), bindings: make(map[string]string)}
}

func (r *fakeRegistryWriter) EnsureProfileRegistry(_ context.Context, _ string, profileRef string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensured = append(r.ensured, ScopeProfile+":"+profileRef)
	return nil
}

func (r *fakeRegistryWriter) EnsureBrowserStateRegistry(_ context.Context, _ string, workspaceRef string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensured = append(r.ensured, ScopeBrowserState+":"+workspaceRef)
	return nil
}

func (r *fakeRegistryWriter) EnsureWorkspaceRegistry(_ context.Context, _ string, workspaceRef string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensured = append(r.ensured, ScopeWorkspace+":"+workspaceRef)
	return nil
}

func (r *fakeRegistryWriter) GetLatestManifestRevision(_ context.Context, scope, ref string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest[scope+":"+ref], nil
}

func (r *fakeRegistryWriter) MarkFlushPending(_ context.Context, scope, ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transitions = append(r.transitions, scope+":"+ref+":pending")
	return nil
}

func (r *fakeRegistryWriter) AcquireFlushLease(_ context.Context, scope, ref, holderID, expectedBaseRevision string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.latest[scope+":"+ref]; current != strings.TrimSpace(expectedBaseRevision) {
		return ErrFlushConflict
	}
	r.bindings[scope+":"+ref] = strings.TrimSpace(holderID)
	r.transitions = append(r.transitions, scope+":"+ref+":running")
	return nil
}

func (r *fakeRegistryWriter) ReleaseFlushFailure(_ context.Context, scope, ref, holderID string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bindings[scope+":"+ref] == strings.TrimSpace(holderID) {
		delete(r.bindings, scope+":"+ref)
	}
	r.transitions = append(r.transitions, scope+":"+ref+":failed")
	return nil
}

func (r *fakeRegistryWriter) CommitFlushSuccess(_ context.Context, scope, ref, holderID, expectedBaseRevision, revision string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bindings[scope+":"+ref] != strings.TrimSpace(holderID) {
		return ErrFlushConflict
	}
	if r.latest[scope+":"+ref] != strings.TrimSpace(expectedBaseRevision) {
		return ErrFlushConflict
	}
	delete(r.bindings, scope+":"+ref)
	r.latest[scope+":"+ref] = revision
	r.transitions = append(r.transitions, scope+":"+ref+":idle")
	return nil
}

func (r *fakeRegistryWriter) ListLatestManifestRevisions(_ context.Context, scope string) ([]RegistryManifestBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]RegistryManifestBinding, 0)
	for key, revision := range r.latest {
		prefix := strings.TrimSpace(scope) + ":"
		if !strings.HasPrefix(key, prefix) || strings.TrimSpace(revision) == "" {
			continue
		}
		items = append(items, RegistryManifestBinding{Ref: strings.TrimPrefix(key, prefix), Revision: revision})
	}
	return items, nil
}

type fakeCarrier struct {
	mu                sync.Mutex
	appliedManifest   map[string]Manifest
	appliedFiles      map[string][]FilePayload
	appliedCount      map[string]int
	appliedPrunePaths map[string][]string
	appliedPruneRoot  map[string]bool
	manifests         map[string]Manifest
	filePayloads      map[string]map[string][]byte
	manifestRequests  map[string][][]string
	fileRequests      map[string][][]string
}

func newFakeCarrier() *fakeCarrier {
	return &fakeCarrier{
		appliedManifest:   make(map[string]Manifest),
		appliedFiles:      make(map[string][]FilePayload),
		appliedCount:      make(map[string]int),
		appliedPrunePaths: make(map[string][]string),
		appliedPruneRoot:  make(map[string]bool),
		manifests:         make(map[string]Manifest),
		filePayloads:      make(map[string]map[string][]byte),
		manifestRequests:  make(map[string][][]string),
		fileRequests:      make(map[string][][]string),
	}
}

func (c *fakeCarrier) BuildEnvironmentManifest(_ context.Context, scope string, subtrees []string) (Manifest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copySubtrees := append([]string(nil), subtrees...)
	c.manifestRequests[scope] = append(c.manifestRequests[scope], copySubtrees)
	manifest := CloneManifest(c.manifests[scope])
	if len(subtrees) == 0 {
		return manifest, nil
	}
	filtered := Manifest{Scope: scope}
	for _, entry := range manifest.Entries {
		if pathMatchesAnySubtree(entry.Path, subtrees) {
			filtered.Entries = append(filtered.Entries, entry)
		}
	}
	return NormalizeManifest(filtered), nil
}

func (c *fakeCarrier) CollectEnvironmentFiles(_ context.Context, scope string, paths []string) ([]FilePayload, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copyPaths := append([]string(nil), paths...)
	c.fileRequests[scope] = append(c.fileRequests[scope], copyPaths)
	manifestEntries := EntryMap(c.manifests[scope].Entries)
	result := make([]FilePayload, 0, len(paths))
	for _, path := range paths {
		entry := manifestEntries[path]
		result = append(result, EncodeFilePayload(path, c.filePayloads[scope][path], entry))
	}
	return result, nil
}

func (c *fakeCarrier) ApplyEnvironment(_ context.Context, scope string, manifest Manifest, files []FilePayload, prunePaths []string, pruneRootChildren bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.appliedManifest[scope] = CloneManifest(manifest)
	c.appliedFiles[scope] = append([]FilePayload(nil), files...)
	c.appliedCount[scope]++
	c.appliedPrunePaths[scope] = append([]string(nil), prunePaths...)
	c.appliedPruneRoot[scope] = pruneRootChildren
	return nil
}

func markScopeDirty(mgr *Manager, sessionID, scope, subtree string) {
	entry := mgr.lookupSession(sessionID)
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	state := entry.scopeLocked(scope)
	state.markDirty(subtree)
	mgr.scheduleScopeLocked(sessionID, scope, state)
}

func TestManagerPrepareWithoutManifestOnlyEnsuresRegistries(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if carrier.appliedCount[ScopeProfile] != 0 || carrier.appliedCount[ScopeBrowserState] != 0 || carrier.appliedCount[ScopeWorkspace] != 0 {
		t.Fatalf("did not expect hydrate without manifest: %#v", carrier.appliedCount)
	}
	if len(registry.ensured) != 3 {
		t.Fatalf("expected registry ensure calls, got %v", registry.ensured)
	}
}

func TestManagerPrepareRestoresFromManifestState(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()

	manifest := NormalizeManifest(Manifest{
		Scope:    ScopeWorkspace,
		Ref:      binding.WorkspaceRef,
		Revision: "rev-1",
		Entries:  []ManifestEntry{{Path: "chart.png", Type: EntryTypeFile, Mode: 0o644, Size: 8, SHA256: "sha-chart"}},
	})
	if err := saveManifest(context.Background(), store, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-chart"), []byte("png-data")); err != nil {
		t.Fatalf("put blob: %v", err)
	}
	registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef] = manifest.Revision

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := carrier.appliedManifest[ScopeWorkspace]; len(got.Entries) != 1 || got.Entries[0].Path != "chart.png" {
		t.Fatalf("unexpected applied manifest: %#v", got)
	}
	if len(carrier.appliedFiles[ScopeWorkspace]) != 1 {
		t.Fatalf("unexpected applied files: %#v", carrier.appliedFiles[ScopeWorkspace])
	}
	if !carrier.appliedPruneRoot[ScopeWorkspace] {
		t.Fatal("expected workspace hydrate to prune root children")
	}
}

func TestManagerPrepareRestoresBrowserStateFromWorkspaceRef(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()

	manifest := NormalizeManifest(Manifest{
		Scope:    ScopeBrowserState,
		Ref:      binding.WorkspaceRef,
		Revision: "rev-browser-1",
		Entries:  []ManifestEntry{{Path: "sessions/test/state.json", Type: EntryTypeFile, Mode: 0o644, Size: 2, SHA256: "sha-browser"}},
	})
	if err := saveManifest(context.Background(), store, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeBrowserState, binding.WorkspaceRef, "sha-browser"), []byte("{}")); err != nil {
		t.Fatalf("put blob: %v", err)
	}
	registry.latest[ScopeBrowserState+":"+binding.WorkspaceRef] = manifest.Revision

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := carrier.appliedManifest[ScopeBrowserState]; len(got.Entries) != 1 || got.Entries[0].Path != "sessions/test/state.json" {
		t.Fatalf("unexpected browser state manifest: %#v", got)
	}
	if !carrier.appliedPruneRoot[ScopeBrowserState] {
		t.Fatal("expected browser_state hydrate to prune root children")
	}
}

func TestManagerPrepareDoesNotRestoreBrowserStateAcrossWorkspaces(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	bindingA := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	bindingB := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_b"}
	carrier := newFakeCarrier()

	manifest := NormalizeManifest(Manifest{
		Scope:    ScopeBrowserState,
		Ref:      bindingA.WorkspaceRef,
		Revision: "rev-browser-1",
		Entries:  []ManifestEntry{{Path: "sessions/test/state.json", Type: EntryTypeFile, Mode: 0o644, Size: 2, SHA256: "sha-browser"}},
	})
	if err := saveManifest(context.Background(), store, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeBrowserState, bindingA.WorkspaceRef, "sha-browser"), []byte("{}")); err != nil {
		t.Fatalf("put blob: %v", err)
	}
	registry.latest[ScopeBrowserState+":"+bindingA.WorkspaceRef] = manifest.Revision

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, bindingB); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if carrier.appliedCount[ScopeBrowserState] != 0 {
		t.Fatalf("expected no browser state hydrate across workspaces, got %#v", carrier.appliedManifest[ScopeBrowserState])
	}
}

func TestManagerMarkAllDirtyMarksAllBoundScopes(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	entry := mgr.ensureSession("sess-1", carrier, binding)

	mgr.MarkAllDirty("sess-1")

	entry.mu.Lock()
	defer entry.mu.Unlock()
	for _, scope := range []string{ScopeProfile, ScopeBrowserState, ScopeWorkspace} {
		state := entry.scopeLocked(scope)
		if !state.hasRootDirty() {
			t.Fatalf("expected %s to be root-dirty", scope)
		}
	}
}

func TestManagerFlushBrowserStateUsesWorkspaceRef(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeBrowserState] = NormalizeManifest(Manifest{
		Scope:   ScopeBrowserState,
		Entries: []ManifestEntry{{Path: "sessions/test/state.json", Type: EntryTypeFile, Mode: 0o644, Size: 2, SHA256: "sha-browser"}},
	})
	carrier.filePayloads[ScopeBrowserState] = map[string][]byte{"sessions/test/state.json": []byte("{}")}

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	markScopeDirty(mgr, "sess-1", ScopeBrowserState, "")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("flush now: %v", err)
	}
	revision := registry.latest[ScopeBrowserState+":"+binding.WorkspaceRef]
	if revision == "" {
		t.Fatal("expected browser state revision bound to workspace")
	}
	if got := registry.latest[ScopeBrowserState+":"+binding.ProfileRef]; got != "" {
		t.Fatalf("expected profile-scoped browser state revision to stay empty, got %q", got)
	}
	manifest, err := loadManifest(context.Background(), store, ScopeBrowserState, binding.WorkspaceRef, revision)
	if err != nil {
		t.Fatalf("load browser state manifest: %v", err)
	}
	if manifest.Ref != binding.WorkspaceRef {
		t.Fatalf("expected workspace ref in browser state manifest, got %q", manifest.Ref)
	}
}

func TestManagerPrepareFailsWhenRegistryManifestMissing(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef] = "rev-missing"

	err := mgr.Prepare(context.Background(), "sess-1", carrier, binding)
	if err == nil {
		t.Fatal("expected prepare to fail when manifest is missing")
	}
}

func TestManagerPrepareFailsWhenRegistryBlobMissing(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	manifest := NormalizeManifest(Manifest{
		Scope:    ScopeWorkspace,
		Ref:      binding.WorkspaceRef,
		Revision: "rev-1",
		Entries:  []ManifestEntry{{Path: "chart.png", Type: EntryTypeFile, Mode: 0o644, Size: 8, SHA256: "sha-chart"}},
	})
	if err := saveManifest(context.Background(), store, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef] = manifest.Revision

	err := mgr.Prepare(context.Background(), "sess-1", carrier, binding)
	if err == nil {
		t.Fatal("expected prepare to fail when blob is missing")
	}
}

func TestManagerPrepareSkipsRehydrateForSameRevision(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	manifest := NormalizeManifest(Manifest{
		Scope:    ScopeWorkspace,
		Ref:      binding.WorkspaceRef,
		Revision: "rev-1",
		Entries:  []ManifestEntry{{Path: "chart.png", Type: EntryTypeFile, Mode: 0o644, Size: 8, SHA256: "sha-chart"}},
	})
	if err := saveManifest(context.Background(), store, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-chart"), []byte("png-data")); err != nil {
		t.Fatalf("put blob: %v", err)
	}
	registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef] = manifest.Revision

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if carrier.appliedCount[ScopeWorkspace] != 1 {
		t.Fatalf("expected one workspace hydrate, got %d", carrier.appliedCount[ScopeWorkspace])
	}
}

func TestManagerPrepareRehydratesWhenRevisionChanges(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	manifest1 := NormalizeManifest(Manifest{
		Scope:    ScopeWorkspace,
		Ref:      binding.WorkspaceRef,
		Revision: "rev-1",
		Entries:  []ManifestEntry{{Path: "chart.png", Type: EntryTypeFile, Mode: 0o644, Size: 8, SHA256: "sha-chart"}},
	})
	manifest2 := NormalizeManifest(Manifest{
		Scope:    ScopeWorkspace,
		Ref:      binding.WorkspaceRef,
		Revision: "rev-2",
		Entries:  []ManifestEntry{{Path: "chart-v2.png", Type: EntryTypeFile, Mode: 0o644, Size: 10, SHA256: "sha-chart-v2"}},
	})
	if err := saveManifest(context.Background(), store, manifest1); err != nil {
		t.Fatalf("save manifest1: %v", err)
	}
	if err := saveManifest(context.Background(), store, manifest2); err != nil {
		t.Fatalf("save manifest2: %v", err)
	}
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-chart"), []byte("png-data")); err != nil {
		t.Fatalf("put blob1: %v", err)
	}
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-chart-v2"), []byte("png-data-v2")); err != nil {
		t.Fatalf("put blob2: %v", err)
	}
	registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef] = manifest1.Revision

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef] = manifest2.Revision
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if carrier.appliedCount[ScopeWorkspace] != 2 {
		t.Fatalf("expected two workspace hydrates, got %d", carrier.appliedCount[ScopeWorkspace])
	}
	if got := carrier.appliedManifest[ScopeWorkspace]; len(got.Entries) != 1 || got.Entries[0].Path != "chart-v2.png" {
		t.Fatalf("unexpected applied manifest after revision change: %#v", got)
	}
}

func TestManagerFlushWritesManifestAndBlob(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}},
	})
	carrier.filePayloads[ScopeWorkspace] = map[string][]byte{"src/main.go": []byte("hello")}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	markScopeDirty(mgr, "sess-1", ScopeWorkspace, "src")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("flush now: %v", err)
	}

	revision := registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef]
	if revision == "" {
		t.Fatalf("expected workspace revision to be recorded")
	}
	manifest, err := loadManifest(context.Background(), store, ScopeWorkspace, binding.WorkspaceRef, revision)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(manifest.Entries) != 1 || manifest.Entries[0].Path != "src/main.go" {
		t.Fatalf("unexpected manifest entries: %#v", manifest.Entries)
	}
	decoded, err := loadBlob(context.Background(), store, blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-main"))
	if err != nil {
		t.Fatalf("load blob: %v", err)
	}
	if string(decoded) != "hello" {
		t.Fatalf("unexpected blob content: %q", decoded)
	}
}

func TestManagerFlushReusesExistingBlobAcrossRevisions(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}},
	})
	carrier.filePayloads[ScopeWorkspace] = map[string][]byte{"src/main.go": []byte("hello")}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	markScopeDirty(mgr, "sess-1", ScopeWorkspace, "src")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	firstRevision := registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef]
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}, {Path: "src/extra.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-extra"}},
	})
	carrier.filePayloads[ScopeWorkspace]["src/extra.go"] = []byte("extra")
	markScopeDirty(mgr, "sess-1", ScopeWorkspace, "src")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	secondRevision := registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef]
	if secondRevision == "" || secondRevision == firstRevision {
		t.Fatalf("expected a new revision, got %q", secondRevision)
	}
	requests := carrier.fileRequests[ScopeWorkspace]
	if len(requests) < 2 {
		t.Fatalf("expected two file collection rounds, got %v", requests)
	}
	if len(requests[1]) != 1 || requests[1][0] != "src/extra.go" {
		t.Fatalf("expected only changed file to be uploaded, got %v", requests[1])
	}
}

func TestManagerFlushMergesDirtySubtreeWithoutRewritingWholeManifest(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}, {Path: "docs/readme.md", Type: EntryTypeFile, Mode: 0o644, Size: 3, SHA256: "sha-docs"}},
	})
	carrier.filePayloads[ScopeWorkspace] = map[string][]byte{"src/main.go": []byte("hello"), "docs/readme.md": []byte("doc")}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	markScopeDirty(mgr, "sess-1", ScopeWorkspace, "")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("seed flush: %v", err)
	}
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 7, SHA256: "sha-main-2"}},
	})
	carrier.filePayloads[ScopeWorkspace]["src/main.go"] = []byte("hello-2")
	markScopeDirty(mgr, "sess-1", ScopeWorkspace, "src")
	entry := mgr.lookupSession("sess-1")
	entry.mu.Lock()
	version := entry.scopeLocked(ScopeWorkspace).version
	entry.mu.Unlock()
	if err := mgr.flushScope(context.Background(), "sess-1", ScopeWorkspace, version, false); err != nil {
		t.Fatalf("incremental flush: %v", err)
	}
	revision := registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef]
	manifest, err := loadManifest(context.Background(), store, ScopeWorkspace, binding.WorkspaceRef, revision)
	if err != nil {
		t.Fatalf("load merged manifest: %v", err)
	}
	if len(manifest.Entries) != 2 {
		t.Fatalf("expected merged manifest entries, got %#v", manifest.Entries)
	}
	if manifest.Entries[0].Path != "docs/readme.md" || manifest.Entries[1].Path != "src/main.go" {
		t.Fatalf("unexpected merged manifest ordering: %#v", manifest.Entries)
	}
}

func TestManagerSweepUnreferencedBlobsDeletesOrphans(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	manifest := NormalizeManifest(Manifest{
		Scope:    ScopeWorkspace,
		Ref:      binding.WorkspaceRef,
		Revision: "rev-1",
		Entries:  []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-live"}},
	})
	if err := saveManifest(context.Background(), store, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef] = manifest.Revision
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-live"), []byte("live")); err != nil {
		t.Fatalf("put live blob: %v", err)
	}
	if _, err := putBlobIfMissing(context.Background(), store, blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-old"), []byte("old")); err != nil {
		t.Fatalf("put old blob: %v", err)
	}
	if err := mgr.SweepUnreferencedBlobs(context.Background()); err != nil {
		t.Fatalf("sweep blobs: %v", err)
	}
	if _, err := store.Get(context.Background(), blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-live")); err != nil {
		t.Fatalf("expected live blob to remain: %v", err)
	}
	if _, err := store.Get(context.Background(), blobKey(ScopeWorkspace, binding.WorkspaceRef, "sha-old")); !objectstore.IsNotFound(err) {
		t.Fatalf("expected orphan blob deleted, got %v", err)
	}
}

func TestManagerFlushLogsStructuredFields(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	var output bytes.Buffer
	logger := logging.NewJSONLogger("test", &output)
	mgr := NewManager(store, registry, logger, Config{})
	binding := Binding{AccountID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}},
	})
	carrier.filePayloads[ScopeWorkspace] = map[string][]byte{"src/main.go": []byte("hello")}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	markScopeDirty(mgr, "sess-1", ScopeWorkspace, "src")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("flush now: %v", err)
	}
	var payload map[string]any
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	if len(lines) == 0 {
		t.Fatal("expected flush log line")
	}
	if err := json.Unmarshal(lines[len(lines)-1], &payload); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	for _, key := range []string{"flush_scope", "flush_ref", "flush_reason", "dirty_count", "pending_bytes", "blob_put_count", "blob_skip_count", "manifest_size", "flush_duration_ms", "flush_result"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing log field %s in %#v", key, payload)
		}
	}
}
