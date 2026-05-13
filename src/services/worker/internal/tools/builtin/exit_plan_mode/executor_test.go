package exit_plan_mode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type bindingStub struct {
	active bool
	path   string
}

func (b *bindingStub) PlanFilePathValue() string {
	return b.path
}

func (b *bindingStub) SetIsPlanMode(active bool) {
	b.active = active
}

func (b *bindingStub) IsPlanModeActive() bool {
	return b.active
}

func TestExitPlanModeDoesNotRequirePlanArgument(t *testing.T) {
	threadID := uuid.New()
	workDir := t.TempDir()
	planPath := "plans/channel_phase_1_0cc67a18.plan.md"
	if err := os.MkdirAll(filepath.Join(workDir, "plans"), 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, planPath), []byte("1. inspect\n2. implement\n"), 0o644); err != nil {
		t.Fatalf("write plan file: %v", err)
	}
	binding := &bindingStub{
		active: true,
		path:   planPath,
	}

	result := New().Execute(context.Background(), ToolName, map[string]any{}, tools.ExecutionContext{
		ThreadID:   &threadID,
		RunID:      uuid.New(),
		Emitter:    events.NewEmitter("trace"),
		WorkDir:    workDir,
		PipelineRC: binding,
	}, "call_1")

	if result.Error != nil {
		t.Fatalf("exit_plan_mode error: %v", result.Error)
	}
	if binding.active {
		t.Fatal("expected binding to leave plan mode")
	}
	if len(result.Events) != 1 || result.Events[0].Type != "thread.collaboration_mode.updated" {
		t.Fatalf("expected collaboration mode event, got %#v", result.Events)
	}
	if got, _ := result.Events[0].DataJSON["collaboration_mode"].(string); got != "default" {
		t.Fatalf("event collaboration_mode = %#v, want default", result.Events[0].DataJSON["collaboration_mode"])
	}
	if got, _ := result.ResultJSON["status"].(string); got != "plan_mode_exited" {
		t.Fatalf("status = %#v, want plan_mode_exited", result.ResultJSON["status"])
	}
	if got, _ := result.ResultJSON["plan"].(string); got != "1. inspect\n2. implement" {
		t.Fatalf("plan = %#v", got)
	}
	if got, _ := result.ResultJSON["plan_file_path"].(string); got != planPath {
		t.Fatalf("plan_file_path = %#v", got)
	}
	if got, _ := result.ResultJSON["next_action"].(string); !strings.Contains(got, "Continue") {
		t.Fatalf("next_action = %#v", got)
	}
	if _, ok := result.ResultJSON["artifact_kind"]; ok {
		t.Fatal("exit_plan_mode result must not declare an artifact")
	}
	if _, ok := result.ResultJSON["artifact_uri"]; ok {
		t.Fatal("exit_plan_mode result must not declare an artifact URI")
	}
	if _, ok := result.ResultJSON["resource"]; ok {
		t.Fatal("exit_plan_mode result must not inject a resource")
	}
}

func TestExitPlanModeRequiresBoundPlanFile(t *testing.T) {
	threadID := uuid.New()
	result := New().Execute(context.Background(), ToolName, map[string]any{}, tools.ExecutionContext{
		ThreadID:   &threadID,
		RunID:      uuid.New(),
		WorkDir:    t.TempDir(),
		PipelineRC: &bindingStub{active: true},
	}, "call_1")

	if result.Error == nil {
		t.Fatal("expected missing bound plan file error")
	}
	if !strings.Contains(result.Error.Message, "plan file has not been created yet") {
		t.Fatalf("unexpected error: %#v", result.Error)
	}
}
