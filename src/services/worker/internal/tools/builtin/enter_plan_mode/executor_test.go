package enter_plan_mode

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type bindingStub struct {
	active bool
}

func (b *bindingStub) SetIsPlanMode(active bool) {
	b.active = active
}

func (b *bindingStub) IsPlanModeActive() bool {
	return b.active
}

func TestEnterPlanModeSetsThreadPlanStateEvent(t *testing.T) {
	threadID := uuid.New()
	binding := &bindingStub{}

	result := New().Execute(context.Background(), ToolName, map[string]any{}, tools.ExecutionContext{
		ThreadID:   &threadID,
		Emitter:    events.NewEmitter("trace"),
		PipelineRC: binding,
	}, "call_1")

	if result.Error != nil {
		t.Fatalf("enter_plan_mode error: %v", result.Error)
	}
	if !binding.active {
		t.Fatal("expected binding to enter plan mode")
	}
	if len(result.Events) != 1 || result.Events[0].Type != "thread.collaboration_mode.updated" {
		t.Fatalf("expected thread.collaboration_mode.updated event, got %#v", result.Events)
	}
	if got, _ := result.Events[0].DataJSON["collaboration_mode"].(string); got != "plan" {
		t.Fatalf("event collaboration_mode = %#v, want plan", result.Events[0].DataJSON["collaboration_mode"])
	}
	if got, _ := result.ResultJSON["plan_directory"].(string); got != tools.DefaultPlanDirectory() {
		t.Fatalf("plan_directory = %#v, want %q", got, tools.DefaultPlanDirectory())
	}
}
