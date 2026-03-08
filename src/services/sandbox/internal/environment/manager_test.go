package environment

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
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
	copied := append([]byte(nil), data...)
	s.data[key] = copied
	return nil
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

type fakeCarrier struct {
	mu      sync.Mutex
	exports map[string][]byte
	imports map[string][]byte
}

func newFakeCarrier() *fakeCarrier {
	return &fakeCarrier{
		exports: make(map[string][]byte),
		imports: make(map[string][]byte),
	}
}

func (c *fakeCarrier) ExportEnvironment(_ context.Context, scope string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.exports[scope]...), nil
}

func (c *fakeCarrier) ImportEnvironment(_ context.Context, scope string, archive []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.imports[scope] = append([]byte(nil), archive...)
	return nil
}

func TestManagerFlushAndPrepareAcrossCarriers(t *testing.T) {
	store := newMemoryStore()
	mgr := NewManager(store, nil)
	binding := Binding{OrgID: "org-1", ProfileRef: "pref_a", WorkspaceRef: "wsref_a"}

	first := newFakeCarrier()
	first.exports[ScopeProfile] = []byte("profile-v1")
	first.exports[ScopeWorkspace] = []byte("workspace-v1")
	if err := mgr.Prepare(context.Background(), "sess-1", first, binding); err != nil {
		t.Fatalf("prepare first carrier failed: %v", err)
	}
	mgr.MarkDirty("sess-1")

	deadline := time.Now().Add(2 * time.Second)
	for {
		store.mu.Lock()
		_, hasProfile := store.data[profileKey(binding.ProfileRef)]
		_, hasWorkspace := store.data[workspaceKey(binding.WorkspaceRef)]
		store.mu.Unlock()
		if hasProfile && hasWorkspace {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected debounced flush to persist profile and workspace")
		}
		time.Sleep(50 * time.Millisecond)
	}

	second := newFakeCarrier()
	if err := mgr.Prepare(context.Background(), "sess-2", second, binding); err != nil {
		t.Fatalf("prepare second carrier failed: %v", err)
	}

	second.mu.Lock()
	defer second.mu.Unlock()
	if string(second.imports[ScopeProfile]) != "profile-v1" {
		t.Fatalf("unexpected imported profile archive: %q", second.imports[ScopeProfile])
	}
	if string(second.imports[ScopeWorkspace]) != "workspace-v1" {
		t.Fatalf("unexpected imported workspace archive: %q", second.imports[ScopeWorkspace])
	}
}
