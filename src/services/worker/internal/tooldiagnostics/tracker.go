package tooldiagnostics

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultStuckThreshold = 30 * time.Second
	defaultWatchInterval  = 10 * time.Second
)

var DefaultTracker = NewTracker(defaultStuckThreshold)

type Tracker struct {
	mu             sync.Mutex
	active         map[string]*activeTool
	stuckThreshold time.Duration
}

type activeTool struct {
	RunID         uuid.UUID
	ToolCallID    string
	ToolName      string
	Phase         string
	StartedAt     time.Time
	PhaseUpdated  time.Time
	NextWarnAfter time.Duration
}

type Snapshot struct {
	RunID           uuid.UUID
	ToolCallID      string
	ToolName        string
	Phase           string
	ElapsedMs       int64
	PhaseElapsedMs  int64
	StartedAt       time.Time
	PhaseUpdatedAt  time.Time
	NextWarnAfterMs int64
}

func NewTracker(stuckThreshold time.Duration) *Tracker {
	if stuckThreshold <= 0 {
		stuckThreshold = defaultStuckThreshold
	}
	return &Tracker{
		active:         map[string]*activeTool{},
		stuckThreshold: stuckThreshold,
	}
}

func (t *Tracker) Start(runID uuid.UUID, toolCallID, toolName string) {
	if t == nil || runID == uuid.Nil || strings.TrimSpace(toolCallID) == "" {
		return
	}
	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[key(runID, toolCallID)] = &activeTool{
		RunID:         runID,
		ToolCallID:    strings.TrimSpace(toolCallID),
		ToolName:      strings.TrimSpace(toolName),
		Phase:         "queued",
		StartedAt:     now,
		PhaseUpdated:  now,
		NextWarnAfter: t.stuckThreshold,
	}
}

func (t *Tracker) UpdatePhase(runID uuid.UUID, toolCallID, phase string) {
	if t == nil || runID == uuid.Nil || strings.TrimSpace(toolCallID) == "" {
		return
	}
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	item := t.active[key(runID, toolCallID)]
	if item == nil {
		return
	}
	if item.Phase != phase {
		item.Phase = phase
		item.PhaseUpdated = time.Now().UTC()
	}
}

func (t *Tracker) Finish(runID uuid.UUID, toolCallID string) {
	if t == nil || runID == uuid.Nil || strings.TrimSpace(toolCallID) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.active, key(runID, toolCallID))
}

func (t *Tracker) ActiveForRun(runID uuid.UUID) []Snapshot {
	if t == nil || runID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Snapshot, 0)
	for _, item := range t.active {
		if item.RunID != runID {
			continue
		}
		out = append(out, item.snapshot(now))
	}
	return out
}

func (t *Tracker) DueStuckSnapshots() []Snapshot {
	if t == nil {
		return nil
	}
	now := time.Now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Snapshot, 0)
	for _, item := range t.active {
		elapsed := now.Sub(item.StartedAt)
		if elapsed < item.NextWarnAfter {
			continue
		}
		out = append(out, item.snapshot(now))
		next := item.NextWarnAfter * 2
		if next < item.NextWarnAfter {
			next = item.NextWarnAfter
		}
		item.NextWarnAfter = next
	}
	return out
}

func StartWatchdog(ctxDone <-chan struct{}, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(defaultWatchInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctxDone:
				return
			case <-ticker.C:
				for _, item := range DefaultTracker.DueStuckSnapshots() {
					logger.Warn("tool.stuck",
						"run_id", item.RunID.String(),
						"tool_call_id", item.ToolCallID,
						"tool_name", item.ToolName,
						"phase", item.Phase,
						"elapsed_ms", item.ElapsedMs,
						"phase_elapsed_ms", item.PhaseElapsedMs,
					)
				}
			}
		}
	}()
}

func (s Snapshot) ToFields() map[string]any {
	return map[string]any{
		"run_id":             s.RunID.String(),
		"tool_call_id":       s.ToolCallID,
		"tool_name":          s.ToolName,
		"phase":              s.Phase,
		"elapsed_ms":         s.ElapsedMs,
		"phase_elapsed_ms":   s.PhaseElapsedMs,
		"started_at":         s.StartedAt.Format(time.RFC3339Nano),
		"phase_updated_at":   s.PhaseUpdatedAt.Format(time.RFC3339Nano),
		"next_warn_after_ms": s.NextWarnAfterMs,
	}
}

func (a *activeTool) snapshot(now time.Time) Snapshot {
	return Snapshot{
		RunID:           a.RunID,
		ToolCallID:      a.ToolCallID,
		ToolName:        a.ToolName,
		Phase:           a.Phase,
		ElapsedMs:       now.Sub(a.StartedAt).Milliseconds(),
		PhaseElapsedMs:  now.Sub(a.PhaseUpdated).Milliseconds(),
		StartedAt:       a.StartedAt,
		PhaseUpdatedAt:  a.PhaseUpdated,
		NextWarnAfterMs: a.NextWarnAfter.Milliseconds(),
	}
}

func key(runID uuid.UUID, toolCallID string) string {
	return runID.String() + ":" + strings.TrimSpace(toolCallID)
}
