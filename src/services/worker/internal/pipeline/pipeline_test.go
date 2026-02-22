package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/worker/internal/pipeline"
)

// TestBuildExecutesInOrder 验证多个中间件按注册顺序执行，terminal 最后执行。
func TestBuildExecutesInOrder(t *testing.T) {
	var order []string

	mw1 := func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		order = append(order, "mw1")
		return next(ctx, rc)
	}
	mw2 := func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		order = append(order, "mw2")
		return next(ctx, rc)
	}
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		order = append(order, "terminal")
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw1, mw2}, terminal)
	if err := h(context.Background(), &pipeline.RunContext{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"mw1", "mw2", "terminal"}
	if len(order) != len(want) {
		t.Fatalf("got order %v, want %v", order, want)
	}
	for i, v := range want {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

// TestBuildPropagatesRunContext 验证前一个中间件对 rc 的修改对后续可见。
func TestBuildPropagatesRunContext(t *testing.T) {
	mw1 := func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		rc.TraceID = "set-by-mw1"
		return next(ctx, rc)
	}

	var gotTraceID string
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		gotTraceID = rc.TraceID
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw1}, terminal)
	if err := h(context.Background(), &pipeline.RunContext{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTraceID != "set-by-mw1" {
		t.Errorf("terminal got TraceID %q, want %q", gotTraceID, "set-by-mw1")
	}
}

// TestBuildShortCircuit 验证中间件不调用 next 时，terminal 和后续中间件不执行。
func TestBuildShortCircuit(t *testing.T) {
	terminalCalled := false
	mw2Called := false

	mw1 := func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		// 故意不调用 next，模拟短路（如权限拒绝）
		return nil
	}
	mw2 := func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		mw2Called = true
		return next(ctx, rc)
	}
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		terminalCalled = true
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw1, mw2}, terminal)
	if err := h(context.Background(), &pipeline.RunContext{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mw2Called {
		t.Error("mw2 should not have been called after short-circuit")
	}
	if terminalCalled {
		t.Error("terminal should not have been called after short-circuit")
	}
}

// TestBuildRunsCleanupAfterNext 验证中间件在 next 返回后可执行清理逻辑（defer 模式）。
func TestBuildRunsCleanupAfterNext(t *testing.T) {
	var log []string

	mw := func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		log = append(log, "before")
		err := next(ctx, rc)
		log = append(log, "after") // 清理/后处理
		return err
	}
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		log = append(log, "terminal")
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), &pipeline.RunContext{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"before", "terminal", "after"}
	if len(log) != len(want) {
		t.Fatalf("got log %v, want %v", log, want)
	}
	for i, v := range want {
		if log[i] != v {
			t.Errorf("log[%d] = %q, want %q", i, log[i], v)
		}
	}
}

// TestBuildEmptyMiddlewares 验证空中间件列表时直接执行 terminal。
func TestBuildEmptyMiddlewares(t *testing.T) {
	sentinel := errors.New("terminal executed")
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		return sentinel
	}

	h := pipeline.Build(nil, terminal)
	err := h(context.Background(), &pipeline.RunContext{})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}
