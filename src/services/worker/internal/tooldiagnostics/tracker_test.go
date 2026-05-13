package tooldiagnostics

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTrackerActiveLifecycle(t *testing.T) {
	tracker := NewTracker(time.Second)
	runID := uuid.New()

	tracker.Start(runID, "call_1", "grep")
	tracker.UpdatePhase(runID, "call_1", "backend.exec")

	active := tracker.ActiveForRun(runID)
	if len(active) != 1 {
		t.Fatalf("expected one active tool, got %d", len(active))
	}
	if active[0].ToolCallID != "call_1" || active[0].ToolName != "grep" || active[0].Phase != "backend.exec" {
		t.Fatalf("unexpected snapshot: %#v", active[0])
	}

	tracker.Finish(runID, "call_1")
	if active := tracker.ActiveForRun(runID); len(active) != 0 {
		t.Fatalf("expected no active tools after finish, got %#v", active)
	}
}

func TestTrackerStuckSnapshotsUseBackoff(t *testing.T) {
	tracker := NewTracker(30 * time.Second)
	runID := uuid.New()
	tracker.Start(runID, "call_1", "read")
	tracker.mu.Lock()
	item := tracker.active[key(runID, "call_1")]
	item.StartedAt = time.Now().UTC().Add(-31 * time.Second)
	item.PhaseUpdated = item.StartedAt
	tracker.mu.Unlock()

	first := tracker.DueStuckSnapshots()
	if len(first) != 1 {
		t.Fatalf("expected first stuck snapshot, got %d", len(first))
	}
	second := tracker.DueStuckSnapshots()
	if len(second) != 0 {
		t.Fatalf("expected backoff to suppress immediate duplicate, got %#v", second)
	}
}
