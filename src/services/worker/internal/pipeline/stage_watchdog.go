package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StageWatchdogTimeoutError 表示 pipeline 在进入 agent loop 之前卡住超过看门狗阈值。
type StageWatchdogTimeoutError struct {
	Timeout time.Duration
}

func (e StageWatchdogTimeoutError) Error() string {
	return fmt.Sprintf("pipeline stage watchdog timeout after %s", e.Timeout)
}

// stageGate 在「进入 terminal(agent loop)」与「看门狗开火」之间二选一，互斥裁决。
type stageGate struct {
	mu      sync.Mutex
	decided bool
}

// enterTerminal 由 terminal handler 入口调用，返回 false 表示看门狗已抢先开火，应放弃执行。
func (g *stageGate) enterTerminal() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.decided {
		return false
	}
	g.decided = true
	return true
}

// tryFire 由看门狗调用，返回 false 表示已进入 agent loop，看门狗不再介入。
func (g *stageGate) tryFire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.decided {
		return false
	}
	g.decided = true
	return true
}

// RunWithStageWatchdog 运行 pipeline，并对「进入 terminal handler(agent loop) 之前」的阶段设一个总时限。
// 若任一前置 stage 卡住超过 timeout，则取消前段 ctx、调用 onTimeout 写终态，并返回 StageWatchdogTimeoutError。
// 进入 agent loop 之后看门狗不再介入（agent loop 由自身 governor 的 idle/wall-clock 超时约束）。
// timeout<=0 时退化为直接运行，无看门狗。
func RunWithStageWatchdog(
	ctx context.Context,
	rc *RunContext,
	middlewares []RunMiddleware,
	terminal RunHandler,
	timeout time.Duration,
	onTimeout func(ctx context.Context) error,
) error {
	if timeout <= 0 {
		return Build(middlewares, terminal)(ctx, rc)
	}

	gate := &stageGate{}
	guardedTerminal := func(c context.Context, rc *RunContext) error {
		if !gate.enterTerminal() {
			return context.Canceled
		}
		return terminal(c, rc)
	}
	handler := Build(middlewares, guardedTerminal)

	pipelineCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- handler(pipelineCtx, rc)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		if !gate.tryFire() {
			// agent loop 已在临界点前进入，看门狗放手，等其正常结束。
			return <-done
		}
		cancel()
		if onTimeout != nil {
			octx, ocancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer ocancel()
			if err := onTimeout(octx); err != nil {
				return err
			}
		}
		return StageWatchdogTimeoutError{Timeout: timeout}
	}
}
