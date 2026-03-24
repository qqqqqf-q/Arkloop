package acptool

import (
	"context"
	"sync"
	"time"

	"arkloop/services/worker/internal/events"
)

type acpHandleStatus string

const (
	acpStatusRunning     acpHandleStatus = "running"
	acpStatusCompleted   acpHandleStatus = "completed"
	acpStatusFailed      acpHandleStatus = "failed"
	acpStatusInterrupted acpHandleStatus = "interrupted"
)

const handleMaxAge = 4 * time.Hour

type acpHandleEntry struct {
	mu sync.Mutex

	handleID    string
	status      acpHandleStatus
	output      string
	errMsg      string
	createdAt   time.Time
	completedAt *time.Time

	cancel   context.CancelFunc
	doneOnce sync.Once
	doneCh   chan struct{}

	cachedEvents []events.RunEvent
	evMu         sync.Mutex
}

func (e *acpHandleEntry) closeDone() {
	e.doneOnce.Do(func() { close(e.doneCh) })
}

type acpHandleStore struct {
	mu      sync.RWMutex
	entries map[string]*acpHandleEntry
}

func newACPHandleStore() *acpHandleStore {
	s := &acpHandleStore{entries: make(map[string]*acpHandleEntry)}
	go s.runCleanup()
	return s
}

func (s *acpHandleStore) create(handleID string, cancel context.CancelFunc) *acpHandleEntry {
	entry := &acpHandleEntry{
		handleID:  handleID,
		status:    acpStatusRunning,
		cancel:    cancel,
		doneCh:    make(chan struct{}),
		createdAt: time.Now(),
	}
	s.mu.Lock()
	s.entries[handleID] = entry
	s.mu.Unlock()
	return entry
}

func (s *acpHandleStore) get(handleID string) *acpHandleEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[handleID]
}

func (s *acpHandleStore) setCompleted(handleID, output string) {
	entry := s.get(handleID)
	if entry == nil {
		return
	}
	entry.mu.Lock()
	entry.status = acpStatusCompleted
	entry.output = output
	now := time.Now()
	entry.completedAt = &now
	entry.mu.Unlock()
	entry.closeDone()
}

func (s *acpHandleStore) setFailed(handleID, errMsg string) {
	entry := s.get(handleID)
	if entry == nil {
		return
	}
	entry.mu.Lock()
	entry.status = acpStatusFailed
	entry.errMsg = errMsg
	now := time.Now()
	entry.completedAt = &now
	entry.mu.Unlock()
	entry.closeDone()
}

func (s *acpHandleStore) setInterrupted(handleID string) {
	entry := s.get(handleID)
	if entry == nil {
		return
	}
	entry.mu.Lock()
	entry.status = acpStatusInterrupted
	now := time.Now()
	entry.completedAt = &now
	entry.mu.Unlock()
	entry.closeDone()
}

func (s *acpHandleStore) runCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.sweepExpired()
	}
}

func (s *acpHandleStore) sweepExpired() {
	cutoff := time.Now().Add(-handleMaxAge)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, entry := range s.entries {
		entry.mu.Lock()
		expired := entry.createdAt.Before(cutoff)
		running := entry.status == acpStatusRunning
		entry.mu.Unlock()
		if expired {
			if running {
				entry.cancel()
			}
			delete(s.entries, id)
		}
	}
}
