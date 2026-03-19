package acp

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRegistryStoreAndGet(t *testing.T) {
	r := NewRegistry()

	entry := RuntimeHandleEntry{
		HostProcessID:     "proc-1",
		ProtocolSessionID: "acp-sess-1",
		OutputCursor:      42,
		AgentVersion:      "v1.2.3",
	}
	r.Store("sandbox-1", entry)

	got := r.Get("sandbox-1")
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.HostProcessID != "proc-1" {
		t.Errorf("HostProcessID = %q, want %q", got.HostProcessID, "proc-1")
	}
	if got.ProtocolSessionID != "acp-sess-1" {
		t.Errorf("ProtocolSessionID = %q, want %q", got.ProtocolSessionID, "acp-sess-1")
	}
	if got.OutputCursor != 42 {
		t.Errorf("OutputCursor = %d, want 42", got.OutputCursor)
	}
	if got.AgentVersion != "v1.2.3" {
		t.Errorf("AgentVersion = %q, want %q", got.AgentVersion, "v1.2.3")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if got.LastUsedAt.IsZero() {
		t.Error("LastUsedAt should not be zero")
	}
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	if got := r.Get("does-not-exist"); got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}
}

func TestRegistryRemove(t *testing.T) {
	r := NewRegistry()
	r.Store("sandbox-1", RuntimeHandleEntry{HostProcessID: "proc-1"})

	r.Remove("sandbox-1")
	if got := r.Get("sandbox-1"); got != nil {
		t.Errorf("expected nil after remove, got %+v", got)
	}
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
}

func TestRegistryRemoveExpired(t *testing.T) {
	r := NewRegistry()

	// Insert an entry and backdate its LastUsedAt.
	r.Store("old", RuntimeHandleEntry{HostProcessID: "old-proc"})
	r.mu.Lock()
	r.entries["old"].LastUsedAt = time.Now().Add(-2 * time.Hour)
	r.mu.Unlock()

	r.Store("fresh", RuntimeHandleEntry{HostProcessID: "fresh-proc"})

	removed := r.RemoveExpired(1 * time.Hour)
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if r.Get("old") != nil {
		t.Error("expected old entry to be removed")
	}
	if r.Get("fresh") == nil {
		t.Error("expected fresh entry to still exist")
	}
}

func TestRegistryStorePreservesCreatedAt(t *testing.T) {
	r := NewRegistry()

	r.Store("sandbox-1", RuntimeHandleEntry{HostProcessID: "proc-1", OutputCursor: 1})
	original := r.Get("sandbox-1")
	originalCreatedAt := original.CreatedAt

	// Small sleep to ensure timestamps differ.
	time.Sleep(5 * time.Millisecond)

	r.Store("sandbox-1", RuntimeHandleEntry{HostProcessID: "proc-1", OutputCursor: 99})
	updated := r.Get("sandbox-1")

	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", updated.CreatedAt, originalCreatedAt)
	}
	if updated.OutputCursor != 99 {
		t.Errorf("OutputCursor = %d, want 99", updated.OutputCursor)
	}
	if !updated.LastUsedAt.After(original.LastUsedAt) || updated.LastUsedAt.Equal(original.LastUsedAt) {
		t.Error("LastUsedAt should have been updated")
	}
}

func TestRegistryLen(t *testing.T) {
	r := NewRegistry()
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}

	r.Store("a", RuntimeHandleEntry{HostProcessID: "p1"})
	r.Store("b", RuntimeHandleEntry{HostProcessID: "p2"})
	if r.Len() != 2 {
		t.Errorf("Len = %d, want 2", r.Len())
	}

	// Overwrite existing key should not change length.
	r.Store("a", RuntimeHandleEntry{HostProcessID: "p1-updated"})
	if r.Len() != 2 {
		t.Errorf("Len = %d after overwrite, want 2", r.Len())
	}

	r.Remove("a")
	if r.Len() != 1 {
		t.Errorf("Len = %d after remove, want 1", r.Len())
	}
}

func TestRegistryConcurrency(t *testing.T) {
	r := NewRegistry()
	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("sandbox-%d", id%10)
			for i := 0; i < iterations; i++ {
				r.Store(key, RuntimeHandleEntry{
					HostProcessID:     fmt.Sprintf("proc-%d", id),
					ProtocolSessionID: fmt.Sprintf("acp-%d", id),
					OutputCursor:      uint64(i),
				})
				r.Get(key)
				r.Len()
				if i%5 == 0 {
					r.RemoveExpired(1 * time.Hour)
				}
				if i%7 == 0 {
					r.Remove(key)
				}
			}
		}(g)
	}

	wg.Wait()

	// No assertion on final state — the test passes if the race detector finds no races.
	t.Logf("final registry size: %d", r.Len())
}
