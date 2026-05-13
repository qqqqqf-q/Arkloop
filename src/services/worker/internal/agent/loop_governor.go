package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"arkloop/services/worker/internal/events"
)

var errRunIdleTimeout = errors.New("run idle timeout")

const (
	ErrorClassRunDeadlineExceeded  = "run.deadline_exceeded"
	ErrorClassRunIdleTimeout       = "run.idle_timeout"
	ErrorClassRunPausedWaitingUser = "run.paused_waiting_user"
	EventTypeRunPaused             = "run.paused"
	EventTypeRunResumed            = "run.resumed"
)

type LoopTermination string

const (
	LoopTerminationNone     LoopTermination = ""
	LoopTerminationDeadline LoopTermination = "deadline"
	LoopTerminationIdle     LoopTermination = "idle"
)

type LoopGovernor struct {
	mu                    sync.Mutex
	runCtx                RunContext
	startedAt             time.Time
	lastActivityAt        time.Time
	lastProgressAt        time.Time
	idleHeartbeatInterval time.Duration
	progressCh            chan struct{}
}

func NewLoopGovernor(runCtx RunContext) *LoopGovernor {
	now := time.Now()
	return &LoopGovernor{
		runCtx:                runCtx,
		startedAt:             now,
		lastActivityAt:        now,
		lastProgressAt:        now,
		idleHeartbeatInterval: runCtx.IdleHeartbeatInterval,
		progressCh:            make(chan struct{}, 1),
	}
}

func (g *LoopGovernor) Touch() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastActivityAt = time.Now()
}

func (g *LoopGovernor) TouchProgress() {
	if g == nil {
		return
	}
	now := time.Now()
	g.mu.Lock()
	g.lastActivityAt = now
	g.lastProgressAt = now
	g.mu.Unlock()
	select {
	case g.progressCh <- struct{}{}:
	default:
	}
}

func (g *LoopGovernor) IdleDuration() time.Duration {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	lastProgressAt := g.lastProgressAt
	g.mu.Unlock()
	return time.Since(lastProgressAt)
}

func (g *LoopGovernor) WithRunTimeouts(parent context.Context) (context.Context, func()) {
	if g == nil {
		return parent, func() {}
	}
	ctx := parent
	stopFns := make([]func(), 0, 2)
	if g.runCtx.RunDeadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.runCtx.RunDeadline)
		stopFns = append(stopFns, cancel)
	}
	if g.runCtx.RunIdleTimeout > 0 {
		var cancel context.CancelCauseFunc
		ctx, cancel = context.WithCancelCause(ctx)
		stop := g.startIdleTimer(ctx, cancel)
		stopFns = append(stopFns, stop)
	}
	return ctx, func() {
		for i := len(stopFns) - 1; i >= 0; i-- {
			stopFns[i]()
		}
	}
}

func (g *LoopGovernor) startIdleTimer(ctx context.Context, cancel context.CancelCauseFunc) func() {
	stopCh := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		timer := time.NewTimer(g.runCtx.RunIdleTimeout)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-g.progressCh:
				idleFor := g.IdleDuration()
				if idleFor >= g.runCtx.RunIdleTimeout {
					cancel(errRunIdleTimeout)
					return
				}
				resetTimer(timer, g.runCtx.RunIdleTimeout-idleFor)
			case <-timer.C:
				idleFor := g.IdleDuration()
				if idleFor >= g.runCtx.RunIdleTimeout {
					cancel(errRunIdleTimeout)
					return
				}
				resetTimer(timer, g.runCtx.RunIdleTimeout-idleFor)
			}
		}
	}()
	return func() {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if duration <= 0 {
		duration = time.Nanosecond
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func (g *LoopGovernor) Check(ctx context.Context, emitter events.Emitter, yield func(events.RunEvent) error) (LoopTermination, error) {
	if g == nil {
		return LoopTerminationNone, nil
	}
	if runIdleTimeoutExceeded(ctx) {
		return LoopTerminationIdle, nil
	}
	if runDeadlineExceeded(ctx) {
		return LoopTerminationDeadline, nil
	}
	if ctx.Err() != nil {
		return LoopTerminationNone, nil
	}
	if g.runCtx.RunDeadline > 0 && time.Since(g.startedAt) >= g.runCtx.RunDeadline {
		return LoopTerminationDeadline, nil
	}
	if g.runCtx.RunIdleTimeout > 0 && g.IdleDuration() >= g.runCtx.RunIdleTimeout {
		return LoopTerminationIdle, nil
	}
	lastActivityAt := g.lastActivity()
	if g.idleHeartbeatInterval > 0 && time.Since(lastActivityAt) >= g.idleHeartbeatInterval {
		g.Touch()
		if err := yield(emitter.Emit("run.idle_heartbeat", map[string]any{
			"idle_ms": time.Since(g.startedAt).Milliseconds(),
		}, nil, nil)); err != nil {
			return LoopTerminationNone, err
		}
	}
	return LoopTerminationNone, nil
}

func (g *LoopGovernor) lastActivity() time.Time {
	if g == nil {
		return time.Time{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastActivityAt
}

func (g *LoopGovernor) WaitForUserInput(
	ctx context.Context,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
	requestID string,
	wait func(context.Context) (string, bool),
) (string, bool, bool, error) {
	if err := yield(emitter.Emit(EventTypeRunPaused, map[string]any{
		"reason":     "waiting_user_input",
		"request_id": requestID,
	}, nil, nil)); err != nil {
		return "", false, false, err
	}

	waitCtx := ctx
	cancel := func() {}
	if g != nil && g.runCtx.PausedInputTimeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, g.runCtx.PausedInputTimeout)
	}
	defer cancel()

	text, ok := wait(waitCtx)
	if !ok {
		return "", false, waitCtx.Err() == context.DeadlineExceeded, nil
	}
	g.TouchProgress()
	if err := yield(emitter.Emit(EventTypeRunResumed, map[string]any{
		"reason":     "user_input_received",
		"request_id": requestID,
	}, nil, nil)); err != nil {
		return "", false, false, err
	}
	return text, true, false, nil
}
