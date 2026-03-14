package acp

import (
	"sync"
	"time"
)

// SessionEntry holds the state of a reusable ACP session.
type SessionEntry struct {
	ProcessID    string
	ACPSessionID string
	Cursor       uint64
	AgentVersion string
	CreatedAt    time.Time
	LastUsedAt   time.Time
}

// Registry is a thread-safe cache of active ACP sessions keyed by sandbox session ID.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*SessionEntry
}

// NewRegistry creates an empty session registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]*SessionEntry),
	}
}

// Store saves or updates a session entry.
func (r *Registry) Store(sandboxSessionID string, entry SessionEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.LastUsedAt = time.Now()
	if existing, ok := r.entries[sandboxSessionID]; ok {
		// Preserve CreatedAt from original entry.
		entry.CreatedAt = existing.CreatedAt
	} else if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	r.entries[sandboxSessionID] = &entry
}

// Get retrieves a session entry if it exists.
func (r *Registry) Get(sandboxSessionID string) *SessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[sandboxSessionID]
	if !ok {
		return nil
	}
	// Return a copy to avoid races.
	cp := *entry
	return &cp
}

// Remove deletes a session entry.
func (r *Registry) Remove(sandboxSessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, sandboxSessionID)
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
