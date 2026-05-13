package tools

import (
	"path/filepath"
	"testing"
	"time"
)

type planModePathStub struct {
	active   bool
	allowed  string
	planPath string
}

func (s planModePathStub) IsPlanModeActive() bool {
	return s.active
}

func (s planModePathStub) PlanModeWritePathAllowed(path string) bool {
	return path == s.allowed
}

func (s planModePathStub) PlanFilePathValue() string {
	return s.planPath
}

func (s *planModePathStub) SetPlanFilePath(path string) {
	s.planPath = path
}

func TestPlanModeWriteBlockedAllowsCurrentPlanFile(t *testing.T) {
	if _, blocked := PlanModeWriteBlocked(planModePathStub{active: true, allowed: "plans/thread.md"}, time.Now(), "plans/thread.md"); blocked {
		t.Fatal("expected current plan file to be allowed")
	}
}

func TestPlanModeWriteBlockedRejectsOtherFiles(t *testing.T) {
	result, blocked := PlanModeWriteBlocked(planModePathStub{active: true, allowed: "plans/thread.md"}, time.Now(), "src/main.go")
	if !blocked {
		t.Fatal("expected non-plan file to be blocked")
	}
	if result.Error == nil {
		t.Fatal("expected blocking error")
	}
}

func TestPlanModeWriteBlockedInactive(t *testing.T) {
	if _, blocked := PlanModeWriteBlocked(planModePathStub{active: false}, time.Now(), "src/main.go"); blocked {
		t.Fatal("expected inactive plan mode to allow writes")
	}
}

func TestPlanModePlanFileMetadata(t *testing.T) {
	planDir := t.TempDir()
	t.Setenv("ARKLOOP_PLAN_DIR", planDir)
	planPath := filepath.Join(planDir, "channel_phase_1_0cc67a18.plan.md")
	stub := &planModePathStub{active: true}

	meta, ok := PlanModePlanFileMetadata(
		stub,
		"",
		planPath,
	)
	if !ok {
		t.Fatal("expected plan file metadata")
	}
	if stub.planPath != planPath {
		t.Fatalf("plan path was not bound: %q", stub.planPath)
	}
	if got, _ := meta["plan_file_path"].(string); got != planPath {
		t.Fatalf("plan_file_path = %#v", got)
	}
	if got, _ := meta["filename"].(string); got != filepath.Base(planPath) {
		t.Fatalf("filename = %#v", got)
	}
	if _, ok := meta["artifact_kind"]; ok {
		t.Fatal("plan metadata must not declare an artifact")
	}
	if _, ok := meta["artifact_uri"]; ok {
		t.Fatal("plan metadata must not declare an artifact URI")
	}
	if _, ok := meta["resource"]; ok {
		t.Fatal("plan metadata must not inject a resource")
	}
}

func TestPlanModePlanFileMetadataRejectsOtherFile(t *testing.T) {
	planDir := t.TempDir()
	t.Setenv("ARKLOOP_PLAN_DIR", planDir)
	if _, ok := PlanModePlanFileMetadata(
		&planModePathStub{active: true},
		"",
		filepath.Join(planDir, "notes.md"),
	); ok {
		t.Fatal("expected non-plan file to be ignored")
	}
}
