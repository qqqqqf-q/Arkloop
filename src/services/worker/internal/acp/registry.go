package acp

import (
	"sync"
	"time"
)

// RuntimeHandleEntry caches a reusable ACP runtime handle inside the worker process.
type RuntimeHandleEntry struct {
	HostProcessID     string
	ProtocolSessionID string
	OutputCursor      uint64
	AgentVersion      string
	CreatedAt         time.Time
	LastUsedAt        time.Time
}

// Registry is a thread-safe cache of active ACP runtime handles keyed by runtime session key.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*RuntimeHandleEntry
}

// NewRegistry creates an empty session registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]*RuntimeHandleEntry),
	}
}

// Store saves or updates a runtime handle entry.
func (r *Registry) Store(runtimeSessionKey string, entry RuntimeHandleEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.LastUsedAt = time.Now()
	if existing, ok := r.entries[runtimeSessionKey]; ok {
		// Preserve CreatedAt from original entry.
		entry.CreatedAt = existing.CreatedAt
	} else if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	r.entries[runtimeSessionKey] = &entry
}

// Get retrieves a runtime handle entry if it exists.
func (r *Registry) Get(runtimeSessionKey string) *RuntimeHandleEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[runtimeSessionKey]
	if !ok {
		return nil
	}
	// Return a copy to avoid races.
	cp := *entry
	return &cp
}

// Remove deletes a session entry.
func (r *Registry) Remove(runtimeSessionKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, runtimeSessionKey)
}

// RemoveExpired removes entries older than the given max age based on LastUsedAt.
func (r *Registry) RemoveExpired(maxAge time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, entry := range r.entries {
		if entry.LastUsedAt.Before(cutoff) {
			delete(r.entries, id)
			removed++
		}
	}
	return removed
}

// Len returns the number of entries in the registry.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
