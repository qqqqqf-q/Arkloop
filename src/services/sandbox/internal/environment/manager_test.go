package environment

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

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
}

func newFakeRegistryWriter() *fakeRegistryWriter {
	return &fakeRegistryWriter{latest: make(map[string]string)}
}

func (r *fakeRegistryWriter) EnsureProfileRegistry(_ context.Context, _ string, profileRef string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensured = append(r.ensured, ScopeProfile+":"+profileRef)
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

func (r *fakeRegistryWriter) MarkFlushRunning(_ context.Context, scope, ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transitions = append(r.transitions, scope+":"+ref+":running")
	return nil
}

func (r *fakeRegistryWriter) MarkFlushFailed(_ context.Context, scope, ref string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transitions = append(r.transitions, scope+":"+ref+":failed")
	return nil
}

func (r *fakeRegistryWriter) MarkFlushSucceeded(_ context.Context, scope, ref, revision string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latest[scope+":"+ref] = revision
	r.transitions = append(r.transitions, scope+":"+ref+":idle")
	return nil
}

type fakeCarrier struct {
	mu               sync.Mutex
	legacyExports    map[string][]byte
	legacyImports    map[string][]byte
	manifests        map[string]Manifest
	filePayloads     map[string]map[string][]byte
	manifestRequests map[string][][]string
	fileRequests     map[string][][]string
}

func newFakeCarrier() *fakeCarrier {
	return &fakeCarrier{
		legacyExports:    make(map[string][]byte),
		legacyImports:    make(map[string][]byte),
		manifests:        make(map[string]Manifest),
		filePayloads:     make(map[string]map[string][]byte),
		manifestRequests: make(map[string][][]string),
		fileRequests:     make(map[string][][]string),
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

func (c *fakeCarrier) ApplyEnvironment(_ context.Context, _ string, _ Manifest, _ []FilePayload, _ bool) error {
	return nil
}

func (c *fakeCarrier) ExportEnvironment(_ context.Context, scope string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.legacyExports[scope]...), nil
}

func (c *fakeCarrier) ImportEnvironment(_ context.Context, scope string, archive []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legacyImports[scope] = append([]byte(nil), archive...)
	return nil
}

func TestManagerPrepareKeepsLegacyRestoreAndEnsuresRegistries(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil)
	binding := Binding{OrgID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	store.data[profileKey(binding.ProfileRef)] = []byte("profile-legacy")
	store.data[workspaceKey(binding.WorkspaceRef)] = []byte("workspace-legacy")
	carrier := newFakeCarrier()

	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if string(carrier.legacyImports[ScopeProfile]) != "profile-legacy" {
		t.Fatalf("unexpected legacy profile import: %q", carrier.legacyImports[ScopeProfile])
	}
	if string(carrier.legacyImports[ScopeWorkspace]) != "workspace-legacy" {
		t.Fatalf("unexpected legacy workspace import: %q", carrier.legacyImports[ScopeWorkspace])
	}
	if len(registry.ensured) != 2 {
		t.Fatalf("expected registry ensure calls, got %v", registry.ensured)
	}
}

func TestManagerFlushWritesManifestBlobAndLatestPointer(t *testing.T) {
	store := newMemoryStore()
	registry := newFakeRegistryWriter()
	mgr := NewManager(store, registry, nil)
	binding := Binding{OrgID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}},
	})
	carrier.filePayloads[ScopeWorkspace] = map[string][]byte{"src/main.go": []byte("hello")}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	mgr.MarkDirty("sess-1", "/workspace/src")
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
	pointer, err := loadLatestPointer(context.Background(), store, ScopeWorkspace, binding.WorkspaceRef)
	if err != nil {
		t.Fatalf("load latest pointer: %v", err)
	}
	if pointer.Revision != revision {
		t.Fatalf("unexpected latest pointer revision: %s", pointer.Revision)
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
	mgr := NewManager(store, registry, nil)
	binding := Binding{OrgID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}},
	})
	carrier.filePayloads[ScopeWorkspace] = map[string][]byte{"src/main.go": []byte("hello")}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	mgr.MarkDirty("sess-1", "/workspace/src")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	firstRevision := registry.latest[ScopeWorkspace+":"+binding.WorkspaceRef]
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}, {Path: "src/extra.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-extra"}},
	})
	carrier.filePayloads[ScopeWorkspace]["src/extra.go"] = []byte("extra")
	mgr.MarkDirty("sess-1", "/workspace/src")
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
	mgr := NewManager(store, registry, nil)
	binding := Binding{OrgID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}
	carrier := newFakeCarrier()
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 5, SHA256: "sha-main"}, {Path: "docs/readme.md", Type: EntryTypeFile, Mode: 0o644, Size: 3, SHA256: "sha-docs"}},
	})
	carrier.filePayloads[ScopeWorkspace] = map[string][]byte{"src/main.go": []byte("hello"), "docs/readme.md": []byte("doc")}
	if err := mgr.Prepare(context.Background(), "sess-1", carrier, binding); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	mgr.MarkDirty("sess-1", "/workspace")
	if err := mgr.FlushNow(context.Background(), "sess-1"); err != nil {
		t.Fatalf("seed flush: %v", err)
	}
	carrier.manifests[ScopeWorkspace] = NormalizeManifest(Manifest{
		Scope:   ScopeWorkspace,
		Entries: []ManifestEntry{{Path: "src/main.go", Type: EntryTypeFile, Mode: 0o644, Size: 7, SHA256: "sha-main-2"}},
	})
	carrier.filePayloads[ScopeWorkspace]["src/main.go"] = []byte("hello-2")
	mgr.MarkDirty("sess-1", "/workspace/src")
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
