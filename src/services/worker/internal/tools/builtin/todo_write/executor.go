package todowrite

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
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
	ID         string `json:"id"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form,omitempty"`
}

// Executor 持有 per-run todo 状态，生命周期与 DispatchingExecutor 相同。
type Executor struct {
	Tracker *fileops.FileTracker

	mu    sync.RWMutex
	state map[string][]TodoItem // runID -> items
}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	planPath, _ := args["plan_path"].(string)
	planPath = strings.TrimSpace(planPath)
	if planPath != "" {
		return e.executePlanBound(ctx, toolName, args, execCtx, planPath, started)
	}

	rawTodos, ok := args["todos"]
	if !ok {
		return errResult(errorArgsInvalid, "parameter todos is required when plan_path is not provided", started)
	}

	items, err := parseTodos(rawTodos)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}

	return e.emitSnapshot(toolName, execCtx, items, started, nil)
}

func (e *Executor) executePlanBound(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	planPath string,
	started time.Time,
) tools.ExecutionResult {
	if _, ok := args["todos"]; ok {
		return errResult(errorArgsInvalid, "parameter todos cannot be used with plan_path; use updates instead", started)
	}
	rawUpdates, ok := args["updates"]
	if !ok {
		return errResult(errorArgsInvalid, "parameter updates is required when plan_path is provided", started)
	}
	updates, err := parsePlanTodoUpdates(rawUpdates)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}
	if blocked, isBlocked := tools.PlanModeWriteBlocked(execCtx.PipelineRC, started, planPath); isBlocked {
		return blocked
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)
	items, err := updatePlanTodos(ctx, backend, planPath, updates)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}
	e.recordPlanWrite(execCtx, backend, planPath)
	return e.emitSnapshot(toolName, execCtx, items, started, map[string]any{
		"plan_path":     planPath,
		"plan_bound":    true,
		"update_count":  len(updates),
		"updated_todos": planUpdateMaps(updates),
	})
}

func (e *Executor) emitSnapshot(
	toolName string,
	execCtx tools.ExecutionContext,
	items []TodoItem,
	started time.Time,
	extra map[string]any,
) tools.ExecutionResult {
	runKey := execCtx.RunID.String()
	e.mu.Lock()
	if e.state == nil {
		e.state = make(map[string][]TodoItem)
	}
	oldItems := cloneTodos(e.state[runKey])
	e.state[runKey] = items
	e.mu.Unlock()

	oldTodoList := todoMaps(oldItems)
	todoList := todoMaps(items)
	changes := todoChangeMaps(oldItems, items)
	completedCount := countStatus(items, statusCompleted)

	payload := map[string]any{
		"todos":           todoList,
		"old_todos":       oldTodoList,
		"changes":         changes,
		"completed_count": completedCount,
		"total_count":     len(items),
	}
	for key, value := range extra {
		payload[key] = value
	}

	ev := execCtx.Emitter.Emit("todo.updated", payload, &toolName, nil)
	result := map[string]any{
		"todos":           todoList,
		"old_todos":       oldTodoList,
		"changes":         changes,
		"count":           len(items),
		"completed_count": completedCount,
		"total_count":     len(items),
	}
	for key, value := range extra {
		result[key] = value
	}

	return tools.ExecutionResult{
		ResultJSON: result,
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
	ids := make(map[string]struct{}, len(slice))
	inProgressCount := 0
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
		if _, exists := ids[id]; exists {
			return nil, fmt.Errorf("todos[%d].id %q is duplicated", i, id)
		}
		ids[id] = struct{}{}

		content, _ := m["content"].(string)
		content = strings.TrimSpace(content)
		if content == "" {
			return nil, fmt.Errorf("todos[%d].content must be a non-empty string", i)
		}

		status, _ := m["status"].(string)
		if !validStatuses[status] {
			return nil, fmt.Errorf("todos[%d].status %q is invalid; must be one of: pending, in_progress, completed, cancelled", i, status)
		}
		if status == statusInProgress {
			inProgressCount++
		}

		activeForm, _ := m["active_form"].(string)
		activeForm = strings.TrimSpace(activeForm)
		items = append(items, TodoItem{ID: id, Content: content, Status: status, ActiveForm: activeForm})
	}
	if inProgressCount > 1 {
		return nil, fmt.Errorf("only one todo can be in_progress at a time")
	}
	return items, nil
}

func cloneTodos(items []TodoItem) []TodoItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]TodoItem, len(items))
	copy(out, items)
	return out
}

func todoMaps(items []TodoItem) []map[string]any {
	out := make([]map[string]any, len(items))
	for i, item := range items {
		entry := map[string]any{
			"id":      item.ID,
			"content": item.Content,
			"status":  item.Status,
		}
		if item.ActiveForm != "" {
			entry["active_form"] = item.ActiveForm
		}
		out[i] = entry
	}
	return out
}

func countStatus(items []TodoItem, status string) int {
	count := 0
	for _, item := range items {
		if item.Status == status {
			count++
		}
	}
	return count
}

func todoChangeMaps(oldItems, newItems []TodoItem) []map[string]any {
	oldByID := make(map[string]TodoItem, len(oldItems))
	oldIndexByID := make(map[string]int, len(oldItems))
	for i, item := range oldItems {
		oldByID[item.ID] = item
		oldIndexByID[item.ID] = i
	}

	newIDs := make(map[string]struct{}, len(newItems))
	changes := make([]map[string]any, 0)
	for i, item := range newItems {
		newIDs[item.ID] = struct{}{}
		oldItem, existed := oldByID[item.ID]
		if !existed {
			change := baseTodoChange("created", item, i)
			changes = append(changes, change)
			continue
		}
		if oldItem.Content == item.Content && oldItem.Status == item.Status && oldItem.ActiveForm == item.ActiveForm {
			continue
		}
		change := baseTodoChange("updated", item, i)
		change["old_content"] = oldItem.Content
		change["previous_status"] = oldItem.Status
		if oldItem.ActiveForm != "" {
			change["old_active_form"] = oldItem.ActiveForm
		}
		changes = append(changes, change)
	}

	for _, item := range oldItems {
		if _, exists := newIDs[item.ID]; exists {
			continue
		}
		change := map[string]any{
			"type":            "removed",
			"id":              item.ID,
			"content":         item.Content,
			"previous_status": item.Status,
			"previous_index":  oldIndexByID[item.ID],
		}
		if item.ActiveForm != "" {
			change["old_active_form"] = item.ActiveForm
		}
		changes = append(changes, change)
	}
	return changes
}

func baseTodoChange(changeType string, item TodoItem, index int) map[string]any {
	change := map[string]any{
		"type":    changeType,
		"id":      item.ID,
		"content": item.Content,
		"status":  item.Status,
		"index":   index,
	}
	if item.ActiveForm != "" {
		change["active_form"] = item.ActiveForm
	}
	return change
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
