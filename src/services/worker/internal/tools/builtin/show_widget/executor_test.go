package showwidget

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/tools"
)

func TestExecuteRequiresExplicitReadMeHandshake(t *testing.T) {
	result := NewToolExecutor().Execute(
		context.Background(),
		"show_widget",
		map[string]any{
			"widget_code": "<div>hello</div>",
		},
		tools.ExecutionContext{GenerativeUIReadMeSeen: true},
		"call_1",
	)

	if result.Error == nil {
		t.Fatal("expected error when handshake flag is missing")
	}
	if result.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
}

func TestExecuteRequiresRunScopedReadMeState(t *testing.T) {
	result := NewToolExecutor().Execute(
		context.Background(),
		"show_widget",
		map[string]any{
			"i_have_seen_read_me": true,
			"widget_code":         "<div>hello</div>",
		},
		tools.ExecutionContext{},
		"call_2",
	)

	if result.Error == nil {
		t.Fatal("expected error when read_me was not loaded in this run")
	}
	if result.Error.ErrorClass != "tool.execution_failed" {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
}

func TestExecuteSucceedsAfterReadMe(t *testing.T) {
	result := NewToolExecutor().Execute(
		context.Background(),
		"show_widget",
		map[string]any{
			"i_have_seen_read_me": true,
			"title":               "demo_widget",
			"widget_code":         "<div>hello</div>",
		},
		tools.ExecutionContext{GenerativeUIReadMeSeen: true},
		"call_3",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["ok"] != true {
		t.Fatalf("unexpected result: %#v", result.ResultJSON)
	}
	if result.ResultJSON["title"] != "demo_widget" {
		t.Fatalf("unexpected title: %#v", result.ResultJSON)
	}
}
