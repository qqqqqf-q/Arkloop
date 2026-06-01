package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStageGate_enterThenFire(t *testing.T) {
	g := &stageGate{}
	if !g.enterTerminal() {
		t.Fatal("first enterTerminal should win")
	}
	if g.tryFire() {
		t.Fatal("tryFire must lose after terminal entered")
	}
}

func TestStageGate_fireThenEnter(t *testing.T) {
	g := &stageGate{}
	if !g.tryFire() {
		t.Fatal("first tryFire should win")
	}
	if g.enterTerminal() {
		t.Fatal("enterTerminal must lose after watchdog fired")
	}
}

func TestRunWithStageWatchdog_firesOnStall(t *testing.T) {
	hang := func(ctx context.Context, rc *RunContext, next RunHandler) error {
		<-ctx.Done()
		return ctx.Err()
	}
	var onTimeoutCalled bool
	var mu sync.Mutex
	terminalCalled := false
	terminal := func(context.Context, *RunContext) error {
		mu.Lock()
		terminalCalled = true
		mu.Unlock()
		return nil
	}

	err := RunWithStageWatchdog(context.Background(), &RunContext{}, []RunMiddleware{hang}, terminal, 20*time.Millisecond, func(context.Context) error {
		mu.Lock()
		onTimeoutCalled = true
		mu.Unlock()
		return nil
	})

	var wderr StageWatchdogTimeoutError
	if !errors.As(err, &wderr) {
		t.Fatalf("expected StageWatchdogTimeoutError, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !onTimeoutCalled {
		t.Fatal("onTimeout should have been invoked")
	}
	if terminalCalled {
		t.Fatal("terminal must not run when a prior stage stalls")
	}
}

func TestRunWithStageWatchdog_completesNormally(t *testing.T) {
	passthrough := func(ctx context.Context, rc *RunContext, next RunHandler) error {
		return next(ctx, rc)
	}
	onTimeoutCalled := false
	terminalCalled := false
	terminal := func(context.Context, *RunContext) error {
		terminalCalled = true
		return nil
	}

	err := RunWithStageWatchdog(context.Background(), &RunContext{}, []RunMiddleware{passthrough, passthrough}, terminal, 5*time.Second, func(context.Context) error {
		onTimeoutCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !terminalCalled {
		t.Fatal("terminal should run on the normal path")
	}
	if onTimeoutCalled {
		t.Fatal("onTimeout must not fire on the normal path")
	}
}

func TestRunWithStageWatchdog_disabledWhenZero(t *testing.T) {
	terminalCalled := false
	terminal := func(context.Context, *RunContext) error {
		terminalCalled = true
		return nil
	}
	err := RunWithStageWatchdog(context.Background(), &RunContext{}, nil, terminal, 0, func(context.Context) error {
		t.Fatal("onTimeout must never fire when watchdog disabled")
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !terminalCalled {
		t.Fatal("terminal should run when watchdog disabled")
	}
}

// 进入 terminal(agent loop) 之后即使耗时超过阈值，看门狗也不得开火。
func TestRunWithStageWatchdog_doesNotKillAfterTerminalEntered(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	terminal := func(context.Context, *RunContext) error {
		close(entered)
		<-release
		return nil
	}
	onTimeoutCalled := false
	done := make(chan error, 1)
	go func() {
		done <- RunWithStageWatchdog(context.Background(), &RunContext{}, nil, terminal, 20*time.Millisecond, func(context.Context) error {
			onTimeoutCalled = true
			return nil
		})
	}()

	<-entered
	time.Sleep(60 * time.Millisecond) // 越过阈值，确认看门狗不介入已进入的 agent loop
	if onTimeoutCalled {
		t.Fatal("watchdog must not fire after terminal entered")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("expected nil error after terminal completes, got %v", err)
	}
}
