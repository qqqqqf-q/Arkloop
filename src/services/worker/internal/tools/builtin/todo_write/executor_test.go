package todowrite

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

func TestExecuteEmitsSnapshotDiffAndProgress(t *testing.T) {
	runID := uuid.New()
	exec := &Executor{}
	ctx := tools.ExecutionContext{
		RunID:   runID,
		Emitter: events.NewEmitter("trace_1"),
	}

	first := exec.Execute(context.Background(), ToolName, map[string]any{
		"todos": []any{
			map[string]any{"id": "a", "content": "Read references", "status": statusPending},
			map[string]any{"id": "b", "content": "Implement backend", "active_form": "Implementing backend", "status": statusInProgress},
		},
	}, ctx, "call_1")
	if first.Error != nil {
		t.Fatalf("unexpected first error: %s", first.Error.Message)
	}
	if len(first.Events) != 1 {
		t.Fatalf("expected one event, got %d", len(first.Events))
	}
	firstData := first.Events[0].DataJSON
	if got := firstData["completed_count"]; got != 0 {
		t.Fatalf("expected completed_count=0, got %v", got)
	}
	if got := firstData["total_count"]; got != 2 {
		t.Fatalf("expected total_count=2, got %v", got)
	}
	if oldTodos := firstData["old_todos"].([]map[string]any); len(oldTodos) != 0 {
		t.Fatalf("expected empty old_todos, got %#v", oldTodos)
	}
	if changes := firstData["changes"].([]map[string]any); len(changes) != 2 || changes[0]["type"] != "created" || changes[1]["type"] != "created" {
		t.Fatalf("expected two created changes, got %#v", changes)
	}

	second := exec.Execute(context.Background(), ToolName, map[string]any{
		"todos": []any{
			map[string]any{"id": "a", "content": "Read references", "status": statusCompleted},
			map[string]any{"id": "b", "content": "Implement backend", "active_form": "Implementing backend", "status": statusInProgress},
		},
	}, ctx, "call_2")
	if second.Error != nil {
		t.Fatalf("unexpected second error: %s", second.Error.Message)
	}

	data := second.Events[0].DataJSON
	if got := data["completed_count"]; got != 1 {
		t.Fatalf("expected completed_count=1, got %v", got)
	}
	if got := data["total_count"]; got != 2 {
		t.Fatalf("expected total_count=2, got %v", got)
	}
	oldTodos := data["old_todos"].([]map[string]any)
	if len(oldTodos) != 2 || oldTodos[0]["status"] != statusPending {
		t.Fatalf("expected old snapshot with pending first todo, got %#v", oldTodos)
	}
	todos := data["todos"].([]map[string]any)
	if len(todos) != 2 || todos[0]["status"] != statusCompleted || todos[1]["active_form"] != "Implementing backend" {
		t.Fatalf("expected new snapshot with active_form, got %#v", todos)
	}
	changes := data["changes"].([]map[string]any)
	if len(changes) != 1 {
		t.Fatalf("expected one change, got %#v", changes)
	}
	change := changes[0]
	if change["type"] != "updated" || change["id"] != "a" || change["previous_status"] != statusPending || change["status"] != statusCompleted || change["index"] != 0 {
		t.Fatalf("unexpected status change payload: %#v", change)
	}
}

func TestExecuteUpdatesPlanBoundTodos(t *testing.T) {
	runID := uuid.New()
	dir := t.TempDir()
	planPath := filepath.Join(dir, "demo.plan.md")
	content := `---
name: Demo Plan
overview: Demo
todos:
  - id: read-code
    content: "Read code"
    status: pending
  - id: write-code
    content: "Write code"
    status: pending
isProject: false
---

# Body
`
	if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write plan fixture: %v", err)
	}

	exec := &Executor{}
	ctx := tools.ExecutionContext{
		RunID:   runID,
		WorkDir: dir,
		Emitter: events.NewEmitter("trace_1"),
	}

	result := exec.Execute(context.Background(), ToolName, map[string]any{
		"plan_path": planPath,
		"updates": []any{
			map[string]any{"todo_id": "read-code", "status": statusCompleted},
			map[string]any{"todo_id": "write-code", "status": statusInProgress},
		},
	}, ctx, "call_1")
	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if got := result.ResultJSON["plan_bound"]; got != true {
		t.Fatalf("expected plan_bound=true, got %v", got)
	}
	if got := result.ResultJSON["completed_count"]; got != 1 {
		t.Fatalf("expected completed_count=1, got %v", got)
	}

	updated, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read updated plan: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, "id: read-code") || !strings.Contains(text, "status: completed") {
		t.Fatalf("expected read-code to be completed, got:\n%s", text)
	}
	if !strings.Contains(text, "id: write-code") || !strings.Contains(text, "status: in_progress") {
		t.Fatalf("expected write-code to be in_progress, got:\n%s", text)
	}

	todos := result.Events[0].DataJSON["todos"].([]map[string]any)
	if len(todos) != 2 || todos[0]["status"] != statusCompleted || todos[1]["status"] != statusInProgress {
		t.Fatalf("expected event todos from plan file, got %#v", todos)
	}
}

func TestParseTodosRejectsDuplicateID(t *testing.T) {
	_, err := parseTodos([]any{
		map[string]any{"id": "a", "content": "One", "status": statusPending},
		map[string]any{"id": "a", "content": "Two", "status": statusPending},
	})
	if err == nil {
		t.Fatalf("expected duplicate id error")
	}
}

func TestParseTodosRejectsMultipleInProgress(t *testing.T) {
	_, err := parseTodos([]any{
		map[string]any{"id": "a", "content": "One", "status": statusInProgress},
		map[string]any{"id": "b", "content": "Two", "status": statusInProgress},
	})
	if err == nil {
		t.Fatalf("expected multiple in_progress error")
	}
}
