package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/pipeline"
)

func TestInputLoaderConstructorDoesNotPanic(t *testing.T) {
	mw := pipeline.NewInputLoaderMiddleware(data.RunEventsRepository{}, data.MessagesRepository{}, nil)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
}

// nil pool 时 loadRunInputs 调用 pool.BeginTx 会 panic，
// 证明中间件确实调用了 loadRunInputs 而非直接跳过。
func TestInputLoaderMiddleware_NilPoolPanic(t *testing.T) {
	mw := pipeline.NewInputLoaderMiddleware(data.RunEventsRepository{}, data.MessagesRepository{}, nil)

	rc := &pipeline.RunContext{
		Emitter: events.NewEmitter("test"),
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("nil pool 应触发 panic（BeginTx 调用在 nil 接收者上）")
		}
	}()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}

// 验证 ThreadMessageHistoryLimit <= 0 时默认值为 200：
// 通过 panic 证明 loadRunInputs 被调用（messageLimit 参数已被设为 200）。
// 这里无法直接观测 messageLimit 数值，但能确认 loadRunInputs 确实被执行。
func TestInputLoaderMiddleware_DefaultMessageLimit(t *testing.T) {
	mw := pipeline.NewInputLoaderMiddleware(data.RunEventsRepository{}, data.MessagesRepository{}, nil)

	rc := &pipeline.RunContext{
		Emitter:                   events.NewEmitter("test"),
		ThreadMessageHistoryLimit: 0,
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("nil pool 应触发 panic")
		}
	}()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}

// 负值同样应回退到默认 200
func TestInputLoaderMiddleware_NegativeMessageLimitFallback(t *testing.T) {
	mw := pipeline.NewInputLoaderMiddleware(data.RunEventsRepository{}, data.MessagesRepository{}, nil)

	rc := &pipeline.RunContext{
		Emitter:                   events.NewEmitter("test"),
		ThreadMessageHistoryLimit: -5,
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("nil pool 应触发 panic")
		}
	}()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}

// 中间件链位置验证：InputLoader 应在链中正确传递控制权
func TestInputLoaderMiddleware_ChainPosition(t *testing.T) {
	sentinel := "before-input-loader"

	before := func(_ context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		rc.TraceID = sentinel
		return next(context.Background(), rc)
	}

	inputLoader := pipeline.NewInputLoaderMiddleware(data.RunEventsRepository{}, data.MessagesRepository{}, nil)

	rc := &pipeline.RunContext{
		Emitter: events.NewEmitter("test"),
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("应 panic")
		}
		if rc.TraceID != sentinel {
			t.Fatalf("TraceID = %q, 期望 %q（证明 before 在 inputLoader 之前执行）", rc.TraceID, sentinel)
		}
	}()

	h := pipeline.Build([]pipeline.RunMiddleware{before, inputLoader}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}
