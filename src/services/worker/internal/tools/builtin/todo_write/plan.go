package todowrite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
	"gopkg.in/yaml.v3"
)

type PlanTodoUpdate struct {
	ID     string
	Status string
}

func parsePlanTodoUpdates(raw any) ([]PlanTodoUpdate, error) {
	slice, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("parameter updates must be an array")
	}
	if len(slice) == 0 {
		return nil, fmt.Errorf("parameter updates must not be empty")
	}
	updates := make([]PlanTodoUpdate, 0, len(slice))
	seen := make(map[string]struct{}, len(slice))
	for i, entry := range slice {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("updates[%d] must be an object", i)
		}
		id, _ := m["todo_id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("updates[%d].todo_id must be a non-empty string", i)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("updates[%d].todo_id %q is duplicated", i, id)
		}
		seen[id] = struct{}{}
		status, _ := m["status"].(string)
		if !validStatuses[status] {
			return nil, fmt.Errorf("updates[%d].status %q is invalid; must be one of: pending, in_progress, completed, cancelled", i, status)
		}
		updates = append(updates, PlanTodoUpdate{ID: id, Status: status})
	}
	return updates, nil
}

func updatePlanTodos(ctx context.Context, backend fileops.Backend, planPath string, updates []PlanTodoUpdate) ([]TodoItem, error) {
	if !strings.HasSuffix(filepath.Base(planPath), ".plan.md") {
		return nil, fmt.Errorf("plan_path must point to a .plan.md file")
	}
	data, err := backend.ReadFile(ctx, planPath)
	if err != nil {
		return nil, fmt.Errorf("read plan failed: %w", err)
	}
	frontMatter, body, err := splitPlanFrontMatter(string(data))
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(frontMatter), &doc); err != nil {
		return nil, fmt.Errorf("parse plan front matter failed: %w", err)
	}
	todosNode, err := planTodosNode(&doc)
	if err != nil {
		return nil, err
	}
	if err := applyPlanTodoUpdates(todosNode, updates); err != nil {
		return nil, err
	}
	items, err := planTodoItems(todosNode)
	if err != nil {
		return nil, err
	}
	if _, err := parseTodos(todoMapsAsAny(items)); err != nil {
		return nil, err
	}
	encoded, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("encode plan front matter failed: %w", err)
	}
	content := "---\n" + strings.TrimRight(string(encoded), "\n") + "\n---\n" + body
	if err := backend.WriteFile(ctx, planPath, []byte(content)); err != nil {
		return nil, fmt.Errorf("write plan failed: %w", err)
	}
	return items, nil
}

func splitPlanFrontMatter(content string) (string, string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", "", fmt.Errorf("plan file must start with YAML front matter")
	}
	rest := strings.TrimPrefix(content, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("plan file front matter is not closed")
	}
	after := rest[end+len("\n---"):]
	if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}
	return rest[:end], after, nil
}

func planTodosNode(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("plan front matter must be a YAML object")
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "todos" {
			if root.Content[i+1].Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("plan front matter todos must be an array")
			}
			return root.Content[i+1], nil
		}
	}
	return nil, fmt.Errorf("plan front matter must include todos")
}

func applyPlanTodoUpdates(todosNode *yaml.Node, updates []PlanTodoUpdate) error {
	byID := make(map[string]PlanTodoUpdate, len(updates))
	for _, update := range updates {
		byID[update.ID] = update
	}
	for _, todoNode := range todosNode.Content {
		id := planTodoField(todoNode, "id")
		if update, ok := byID[id]; ok {
			setPlanTodoField(todoNode, "status", update.Status)
			delete(byID, id)
		}
	}
	if len(byID) > 0 {
		missing := make([]string, 0, len(byID))
		for id := range byID {
			missing = append(missing, id)
		}
		return fmt.Errorf("plan todo id not found: %s", strings.Join(missing, ", "))
	}
	return nil
}

func planTodoItems(todosNode *yaml.Node) ([]TodoItem, error) {
	items := make([]TodoItem, 0, len(todosNode.Content))
	for i, todoNode := range todosNode.Content {
		if todoNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("todos[%d] must be an object", i)
		}
		item := TodoItem{
			ID:      strings.TrimSpace(planTodoField(todoNode, "id")),
			Content: strings.TrimSpace(planTodoField(todoNode, "content")),
			Status:  strings.TrimSpace(planTodoField(todoNode, "status")),
		}
		if item.ID == "" || item.Content == "" || item.Status == "" {
			return nil, fmt.Errorf("todos[%d] must include id, content, and status", i)
		}
		items = append(items, item)
	}
	return items, nil
}

func planTodoField(todoNode *yaml.Node, field string) string {
	if todoNode == nil || todoNode.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(todoNode.Content); i += 2 {
		if todoNode.Content[i].Value == field {
			return todoNode.Content[i+1].Value
		}
	}
	return ""
}

func setPlanTodoField(todoNode *yaml.Node, field string, value string) {
	for i := 0; i+1 < len(todoNode.Content); i += 2 {
		if todoNode.Content[i].Value == field {
			todoNode.Content[i+1].Kind = yaml.ScalarNode
			todoNode.Content[i+1].Tag = "!!str"
			todoNode.Content[i+1].Value = value
			return
		}
	}
	todoNode.Content = append(todoNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func todoMapsAsAny(items []TodoItem) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":      item.ID,
			"content": item.Content,
			"status":  item.Status,
		})
	}
	return out
}

func planUpdateMaps(updates []PlanTodoUpdate) []map[string]any {
	out := make([]map[string]any, len(updates))
	for i, update := range updates {
		out[i] = map[string]any{
			"todo_id": update.ID,
			"status":  update.Status,
		}
	}
	return out
}

func (e *Executor) recordPlanWrite(execCtx tools.ExecutionContext, backend fileops.Backend, planPath string) {
	if e == nil || e.Tracker == nil {
		return
	}
	key := backend.NormalizePath(planPath)
	e.Tracker.RecordWriteForRun(execCtx.RunID.String(), key)
	e.Tracker.InvalidateReadState(execCtx.RunID.String(), key)
}

func resolveAccountID(execCtx tools.ExecutionContext) string {
	if execCtx.AccountID == nil {
		return ""
	}
	return execCtx.AccountID.String()
}
