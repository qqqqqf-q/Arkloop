//go:build desktop

package desktoprun

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestActivityRecorderIntervalFromSettings(t *testing.T) {
	if got := activityRecorderIntervalFromSettings(`{"builder_interval_min":45}`); got != 45 {
		t.Fatalf("interval = %d, want 45", got)
	}
	if got := activityRecorderIntervalFromSettings(`{"builder_interval_min":1}`); got != activityRecorderMinIntervalMin {
		t.Fatalf("interval min clamp = %d, want %d", got, activityRecorderMinIntervalMin)
	}
	if got := activityRecorderIntervalFromSettings(`{`); got != activityRecorderDefaultIntervalMin {
		t.Fatalf("bad json interval = %d, want %d", got, activityRecorderDefaultIntervalMin)
	}
}

func TestActivityRecorderRunDataUsesBuilderPersonaAndWindow(t *testing.T) {
	start := time.Date(2026, 5, 19, 1, 2, 3, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	row := activityRecorderDueRow{
		AccountID:    uuid.New(),
		UserID:       uuid.New(),
		ProfileRef:   "profile_local",
		WorkspaceRef: "workspace_local",
		IntervalMin:  activityRecorderDefaultIntervalMin,
	}

	data := activityRecorderRunData(row, start, end)
	if data["run_kind"] != "activity_recorder" {
		t.Fatalf("run_kind = %#v", data["run_kind"])
	}
	if data["persona_id"] != activityRecorderBuilderPersonaID {
		t.Fatalf("persona_id = %#v", data["persona_id"])
	}
	if data["window_start"] != start.Format(time.RFC3339) {
		t.Fatalf("window_start = %#v", data["window_start"])
	}
	if data["window_end"] != end.Format(time.RFC3339) {
		t.Fatalf("window_end = %#v", data["window_end"])
	}
}
