package arkloophelp

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/tools"
)

func TestExecutorReturnsChunksForElectronQuery(t *testing.T) {
	res := Executor{}.Execute(context.Background(), "arkloop_help", map[string]any{
		"query": "Electron Desktop",
	}, tools.ExecutionContext{}, "call")
	if res.Error != nil {
		t.Fatalf("unexpected error: %+v", res.Error)
	}
	var rows []map[string]any
	switch list := res.ResultJSON["chunks"].(type) {
	case []any:
		for _, it := range list {
			m, ok2 := it.(map[string]any)
			if !ok2 {
				t.Fatalf("chunk not map: %#v", it)
			}
			rows = append(rows, m)
		}
	case []map[string]any:
		rows = list
	default:
		t.Fatalf("unexpected chunks type %T: %#v", res.ResultJSON["chunks"], res.ResultJSON["chunks"])
	}
	if len(rows) == 0 {
		t.Fatalf("expected non-empty chunks")
	}
	for _, m := range rows {
		text, _ := m["text"].(string)
		title, _ := m["title"].(string)
		combined := strings.ToLower(title + "\n" + text)
		if strings.Contains(combined, "electron") {
			return
		}
	}
	t.Fatal("expected at least one chunk mentioning Electron")
}
