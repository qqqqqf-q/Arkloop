package pipeline

import (
	"path/filepath"
	"testing"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

func TestPlanModeWritePathAllowedAcceptsAbsolutePlanPath(t *testing.T) {
	planDir := t.TempDir()
	t.Setenv("ARKLOOP_PLAN_DIR", planDir)
	threadID := uuid.New()
	planPath := filepath.Join(planDir, "channel_phase_1_0cc67a18.plan.md")
	rc := &RunContext{
		Run: data.Run{
			ThreadID: threadID,
		},
	}

	if !rc.PlanModeWritePathAllowed(planPath) {
		t.Fatal("expected new plan file under plan directory to be writable")
	}
	if rc.PlanModeWritePathAllowed(filepath.Join(planDir, "notes.md")) {
		t.Fatal("expected non-plan markdown file to remain blocked")
	}

	rc.SetPlanFilePath(planPath)
	if !rc.PlanModeWritePathAllowed(planPath) {
		t.Fatal("expected bound plan path to remain writable")
	}
	if rc.PlanModeWritePathAllowed(filepath.Join(planDir, "other_0cc67a18.plan.md")) {
		t.Fatal("expected second plan file to be blocked after binding")
	}
}
