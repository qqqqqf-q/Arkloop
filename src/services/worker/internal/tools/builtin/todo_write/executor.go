package todowrite

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid = "tool.args_invalid"

	statusPending    = "pending"
	statusInProgress = "in_progress"
	statusCompleted  = "completed"
	statusCancelled  = "cancelled"
)

var validStatuses = map[string]bool{
	statusPending:    true,
	statusInProgress: true,
	statusCompleted:  true,
	statusCancelled:  true,
}

type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// Executor 持有 per-run todo 状态，生命周期与 DispatchingExecutor 相同。
type Executor struct {
	mu    sync.RWMutex
	state map[string][]TodoItem // runID → items
}

func (e *Executor) Execute(
	_ context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	rawTodos, ok := args["todos"]
	if !ok {
		return errResult(errorArgsInvalid, "parameter todos is required", started)
	}

	items, err := parseTodos(rawTodos)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}

	runKey := execCtx.RunID.String()
	e.mu.Lock()
	if e.state == nil {
		e.state = make(map[string][]TodoItem)
	}
	e.state[runKey] = items
	e.mu.Unlock()

	todoList := make([]map[string]any, len(items))
	for i, item := range items {
		todoList[i] = map[string]any{
			"id":      item.ID,
			"content": item.Content,
			"status":  item.Status,
		}
	}

	ev := execCtx.Emitter.Emit(
		"todo.updated",
		map[string]any{"todos": todoList},
		&toolName,
		nil,
	)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"todos": todoList,
			"count": len(items),
		},
		Events:     []events.RunEvent{ev},
		DurationMs: durationMs(started),
	}
}

func parseTodos(raw any) ([]TodoItem, error) {
	slice, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("parameter todos must be an array")
	}

	items := make([]TodoItem, 0, len(slice))
	for i, entry := range slice {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("todos[%d] must be an object", i)
		}

		id, _ := m["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("todos[%d].id must be a non-empty string", i)
		}

		content, _ := m["content"].(string)
		content = strings.TrimSpace(content)
		if content == "" {
			return nil, fmt.Errorf("todos[%d].content must be a non-empty string", i)
		}

		status, _ := m["status"].(string)
		if !validStatuses[status] {
			return nil, fmt.Errorf("todos[%d].status %q is invalid; must be one of: pending, in_progress, completed, cancelled", i, status)
		}

		items = append(items, TodoItem{ID: id, Content: content, Status: status})
	}
	return items, nil
}

func errResult(errorClass, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
		},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}
