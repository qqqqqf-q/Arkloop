package acptool

import (
	"context"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/acp"
	"arkloop/services/worker/internal/events"
)

type acpHandleStatus string

const (
	acpStatusRunning     acpHandleStatus = "running"
	acpStatusIdle        acpHandleStatus = "idle"      // session 存活，当前无 turn
	acpStatusCompleted   acpHandleStatus = "completed" // turn 完成（进程已关则为 terminal）
	acpStatusFailed      acpHandleStatus = "failed"
	acpStatusInterrupted acpHandleStatus = "interrupted"
	acpStatusClosed      acpHandleStatus = "closed" // 进程已关，terminal
)

const handleMaxAge = 4 * time.Hour

type acpHandleEntry struct {
	mu sync.Mutex

	handleID    string
	runID       string
	status      acpHandleStatus
	output      string
	errMsg      string
	createdAt   time.Time
	completedAt *time.Time

	goroutineCtx    context.Context    // 进程级 context，send_acp 用于派生 turn 子 context
	goroutineCancel context.CancelFunc // 进程级 cancel，close_acp / sweepExpired 用
	turnCancel      context.CancelFunc // turn 级 cancel，interrupt_acp 用，每次 turn 更新

	doneOnce sync.Once
	doneCh   chan struct{}

	bridge   *acp.Bridge // 非 nil 表示进程存活
	bridgeMu sync.Mutex

	cachedEvents []events.RunEvent
	evMu         sync.Mutex
}

func (e *acpHandleEntry) closeDone() {
	e.doneOnce.Do(func() { close(e.doneCh) })
}

// resetTurn 在 send_acp 发起新 turn 前调用，重置本次 turn 的状态。
func (e *acpHandleEntry) resetTurn(turnCancel context.CancelFunc) {
	e.mu.Lock()
	e.status = acpStatusRunning
	e.output = ""
	e.errMsg = ""
	e.completedAt = nil
	e.turnCancel = turnCancel
	e.doneCh = make(chan struct{})
	e.doneOnce = sync.Once{}
	e.mu.Unlock()

	e.evMu.Lock()
	e.cachedEvents = nil
	e.evMu.Unlock()
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

func (s *acpHandleStore) create(handleID string, runID string, goroutineCtx context.Context, goroutineCancel context.CancelFunc) *acpHandleEntry {
	entry := &acpHandleEntry{
		handleID:        handleID,
		runID:           strings.TrimSpace(runID),
		status:          acpStatusRunning,
		goroutineCtx:    goroutineCtx,
		goroutineCancel: goroutineCancel,
		doneCh:          make(chan struct{}),
		createdAt:       time.Now(),
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

func (s *acpHandleStore) setIdle(handleID string) {
	entry := s.get(handleID)
	if entry == nil {
		return
	}
	entry.mu.Lock()
	entry.status = acpStatusIdle
	now := time.Now()
	entry.completedAt = &now
	entry.mu.Unlock()
	entry.closeDone()
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

func (s *acpHandleStore) setClosed(handleID string) {
	entry := s.get(handleID)
	if entry == nil {
		return
	}
	entry.mu.Lock()
	entry.status = acpStatusClosed
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

func (s *acpHandleStore) cleanupRun(runID string) int {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return 0
	}

	entries := make([]*acpHandleEntry, 0)
	s.mu.Lock()
	for id, entry := range s.entries {
		entry.mu.Lock()
		sameRun := entry.runID == runID
		entry.mu.Unlock()
		if !sameRun {
			continue
		}
		delete(s.entries, id)
		entries = append(entries, entry)
	}
	s.mu.Unlock()

	for _, entry := range entries {
		entry.goroutineCancel()

		entry.bridgeMu.Lock()
		bridge := entry.bridge
		entry.bridge = nil
		entry.bridgeMu.Unlock()
		if bridge != nil {
			bridge.Close()
		}

		entry.mu.Lock()
		entry.status = acpStatusClosed
		now := time.Now()
		entry.completedAt = &now
		entry.mu.Unlock()
		entry.closeDone()
	}
	return len(entries)
}

func (s *acpHandleStore) sweepExpired() {
	cutoff := time.Now().Add(-handleMaxAge)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, entry := range s.entries {
		entry.mu.Lock()
		expired := entry.createdAt.Before(cutoff)
		status := entry.status
		entry.mu.Unlock()
		if expired {
			if status == acpStatusRunning || status == acpStatusIdle {
				// 触发进程级 cancel，让 goroutine 处理关闭
				entry.goroutineCancel()
			}
			delete(s.entries, id)
		}
	}
}
