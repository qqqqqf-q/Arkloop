package consumer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker/internal/queue"
	"github.com/google/uuid"
)

func TestRunOnceNacksWhenRunLockIsBusy(t *testing.T) {
	lease := queue.JobLease{
		JobID:       uuid.New(),
		JobType:     queue.RunExecuteJobType,
		PayloadJSON: map[string]any{"type": queue.RunExecuteJobType, "run_id": uuid.New().String()},
		Attempts:    1,
		LeaseToken:  uuid.New(),
	}

	fakeQueue := &stubQueue{lease: &lease}
	handler := &stubHandler{}
	locker := &stubLocker{acquired: false}

	loop := newLoopForTest(t, fakeQueue, handler, locker, Config{
		Concurrency:      1,
		PollSeconds:      0,
		LeaseSeconds:     30,
		HeartbeatSeconds: 0,
		QueueJobTypes:    []string{queue.RunExecuteJobType},
	})

	processed, err := loop.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !processed {
		t.Fatal("RunOnce should process one job")
	}
	if handler.called != 0 {
		t.Fatalf("handler should not be called, got %d", handler.called)
	}
	if fakeQueue.ackCount != 0 {
		t.Fatalf("ack should not be called, got %d", fakeQueue.ackCount)
	}
	if fakeQueue.nackCount != 1 {
		t.Fatalf("nack should be called once, got %d", fakeQueue.nackCount)
	}
	if len(fakeQueue.nackDelays) != 1 || fakeQueue.nackDelays[0] != -1 {
		t.Fatalf("unexpected nack delay: %+v", fakeQueue.nackDelays)
	}
}

func TestRunOnceNacksAfterHeartbeatConsecutiveFailures(t *testing.T) {
	lease := queue.JobLease{
		JobID:       uuid.New(),
		JobType:     queue.RunExecuteJobType,
		PayloadJSON: map[string]any{"type": queue.RunExecuteJobType, "run_id": uuid.New().String()},
		LeaseToken:  uuid.New(),
	}

	fakeQueue := &stubQueue{
		lease: &lease,
		heartbeatErrors: []error{
			errors.New("network failed"),
			errors.New("network failed"),
			errors.New("network failed"),
		},
	}
	handler := &stubHandler{blockUntilCancel: true}
	locker := &stubLocker{acquired: true}

	loop := newLoopForTest(t, fakeQueue, handler, locker, Config{
		Concurrency:      1,
		PollSeconds:      0,
		LeaseSeconds:     1,
		HeartbeatSeconds: 0.01,
		QueueJobTypes:    []string{queue.RunExecuteJobType},
	})

	processed, err := loop.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !processed {
		t.Fatal("RunOnce should process one job")
	}
	if fakeQueue.ackCount != 0 {
		t.Fatalf("ack should not be called, got %d", fakeQueue.ackCount)
	}
	if fakeQueue.nackCount != 1 {
		t.Fatalf("nack should be called once, got %d", fakeQueue.nackCount)
	}
	if fakeQueue.heartbeatCount < 3 {
		t.Fatalf("heartbeat should be called at least 3 times, got %d", fakeQueue.heartbeatCount)
	}
}

func TestRunOnceStopsOnLeaseLostWithoutAckOrNack(t *testing.T) {
	lease := queue.JobLease{
		JobID:       uuid.New(),
		JobType:     queue.RunExecuteJobType,
		PayloadJSON: map[string]any{"type": queue.RunExecuteJobType, "run_id": uuid.New().String()},
		LeaseToken:  uuid.New(),
	}

	fakeQueue := &stubQueue{
		lease: &lease,
		heartbeatErrors: []error{
			&queue.LeaseLostError{JobID: lease.JobID},
		},
	}
	handler := &stubHandler{blockUntilCancel: true}
	locker := &stubLocker{acquired: true}

	loop := newLoopForTest(t, fakeQueue, handler, locker, Config{
		Concurrency:      1,
		PollSeconds:      0,
		LeaseSeconds:     1,
		HeartbeatSeconds: 0.01,
		QueueJobTypes:    []string{queue.RunExecuteJobType},
	})

	processed, err := loop.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !processed {
		t.Fatal("RunOnce should process one job")
	}
	if fakeQueue.ackCount != 0 {
		t.Fatalf("ack should not be called, got %d", fakeQueue.ackCount)
	}
	if fakeQueue.nackCount != 0 {
		t.Fatalf("nack should not be called, got %d", fakeQueue.nackCount)
	}
}

func TestRun_ShutdownDoesNotReturnContextCanceled(t *testing.T) {
	for i := 0; i < 30; i++ {
		started := make(chan struct{})
		fakeQueue := &cancelLeaseQueue{started: started}
		handler := &stubHandler{}

		logger := slog.Default()
		loop, err := NewLoop(fakeQueue, handler, nil, Config{
			Concurrency:      1,
			PollSeconds:      0,
			LeaseSeconds:     30,
			HeartbeatSeconds: 0,
			QueueJobTypes:    []string{queue.RunExecuteJobType},
		}, logger, nil)
		if err != nil {
			t.Fatalf("NewLoop failed: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- loop.Run(ctx)
		}()

		select {
		case <-started:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatal("Lease 未启动")
		}

		cancel()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Run 未在预期时间内退出")
		}
	}
}

func TestEvaluateScalingScalesUpWhenWorkersBusy(t *testing.T) {
	fakeQueue := &stubQueue{}
	handler := &stubHandler{}
	loop := newLoopForTest(t, fakeQueue, handler, nil, Config{
		Concurrency:       1,
		PollSeconds:       0,
		LeaseSeconds:      30,
		HeartbeatSeconds:  0,
		QueueJobTypes:     []string{queue.RunExecuteJobType},
		MinConcurrency:    1,
		MaxConcurrency:    8,
		ScaleIntervalSecs: 1,
		ScaleCooldownSecs: 1,
	})
	fakeQueue.stats = queue.QueueStats{
		ReadyDepth: 5,
		InFlight:   1,
	}
	loop.targetWorkers.Store(1)

	next := loop.evaluateScaling(context.Background(), 1)
	if next != 2 {
		t.Fatalf("expected target 2, got %d", next)
	}
}

func TestEvaluateScalingScalesDownWhenIdle(t *testing.T) {
	fakeQueue := &stubQueue{}
	handler := &stubHandler{}
	loop := newLoopForTest(t, fakeQueue, handler, nil, Config{
		Concurrency:       3,
		PollSeconds:       0,
		LeaseSeconds:      30,
		HeartbeatSeconds:  0,
		QueueJobTypes:     []string{queue.RunExecuteJobType},
		MinConcurrency:    1,
		MaxConcurrency:    8,
		ScaleIntervalSecs: 1,
		ScaleCooldownSecs: 1,
	})
	fakeQueue.stats = queue.QueueStats{
		ReadyDepth: 0,
		InFlight:   1,
	}
	loop.targetWorkers.Store(3)

	next := loop.evaluateScaling(context.Background(), 3)
	if next != 2 {
		t.Fatalf("expected target 2, got %d", next)
	}
}

func TestLeaseLostWaitsForHandlerBeforeUnlock(t *testing.T) {
	exitGate := make(chan struct{})
	lease := queue.JobLease{
		JobID:       uuid.New(),
		JobType:     queue.RunExecuteJobType,
		PayloadJSON: map[string]any{"type": queue.RunExecuteJobType, "run_id": uuid.New().String()},
		LeaseToken:  uuid.New(),
	}

	fakeQueue := &stubQueue{
		lease: &lease,
		heartbeatErrors: []error{
			&queue.LeaseLostError{JobID: lease.JobID},
		},
	}
	handler := &stubHandler{blockUntilCancel: true, exitGate: exitGate}
	locker := &stubLocker{acquired: true}

	loop := newLoopForTest(t, fakeQueue, handler, locker, Config{
		Concurrency:      1,
		PollSeconds:      0,
		LeaseSeconds:     1,
		HeartbeatSeconds: 0.01,
		QueueJobTypes:    []string{queue.RunExecuteJobType},
	})

	done := make(chan struct{})
	go func() {
		if _, err := loop.RunOnce(context.Background()); err != nil {
			t.Errorf("RunOnce: %v", err)
		}
		close(done)
	}()

	// handler 被 cancel 后还没退出（exitGate 没关），RunOnce 应该仍在阻塞
	time.Sleep(200 * time.Millisecond)
	if locker.getUnlockCalls() != 0 {
		t.Fatal("lock should not be released while handler is still running")
	}
	select {
	case <-done:
		t.Fatal("RunOnce should still be blocked waiting for handler")
	default:
	}

	// 释放 handler
	close(exitGate)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunOnce did not return after handler exited")
	}

	if locker.getUnlockCalls() != 1 {
		t.Fatalf("lock should be released exactly once after handler exits, got %d", locker.getUnlockCalls())
	}
	if fakeQueue.ackCount != 0 {
		t.Fatalf("ack should not be called on lease_lost, got %d", fakeQueue.ackCount)
	}
	if fakeQueue.nackCount != 0 {
		t.Fatalf("nack should not be called on lease_lost, got %d", fakeQueue.nackCount)
	}
}

func TestHeartbeatFailureWaitsForHandlerBeforeNack(t *testing.T) {
	exitGate := make(chan struct{})
	lease := queue.JobLease{
		JobID:       uuid.New(),
		JobType:     queue.RunExecuteJobType,
		PayloadJSON: map[string]any{"type": queue.RunExecuteJobType, "run_id": uuid.New().String()},
		LeaseToken:  uuid.New(),
	}

	fakeQueue := &stubQueue{
		lease: &lease,
		heartbeatErrors: []error{
			errors.New("network failed"),
			errors.New("network failed"),
			errors.New("network failed"),
		},
	}
	handler := &stubHandler{blockUntilCancel: true, exitGate: exitGate}
	locker := &stubLocker{acquired: true}

	loop := newLoopForTest(t, fakeQueue, handler, locker, Config{
		Concurrency:      1,
		PollSeconds:      0,
		LeaseSeconds:     1,
		HeartbeatSeconds: 0.01,
		QueueJobTypes:    []string{queue.RunExecuteJobType},
	})

	done := make(chan struct{})
	go func() {
		if _, err := loop.RunOnce(context.Background()); err != nil {
			t.Errorf("RunOnce: %v", err)
		}
		close(done)
	}()

	// handler 还没退出，nack 不应被调用
	time.Sleep(200 * time.Millisecond)
	if fakeQueue.nackCount != 0 {
		t.Fatal("nack should not be called while handler is still running")
	}

	close(exitGate)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunOnce did not return after handler exited")
	}

	if fakeQueue.nackCount != 1 {
		t.Fatalf("nack should be called once after handler exits, got %d", fakeQueue.nackCount)
	}
	if locker.getUnlockCalls() != 1 {
		t.Fatalf("lock should be released exactly once, got %d", locker.getUnlockCalls())
	}
}

func TestEvaluateScalingRespectsCooldown(t *testing.T) {
	fakeQueue := &stubQueue{}
	handler := &stubHandler{}
	loop := newLoopForTest(t, fakeQueue, handler, nil, Config{
		Concurrency:       1,
		PollSeconds:       0,
		LeaseSeconds:      30,
		HeartbeatSeconds:  0,
		QueueJobTypes:     []string{queue.RunExecuteJobType},
		MinConcurrency:    1,
		MaxConcurrency:    8,
		ScaleIntervalSecs: 1,
		ScaleCooldownSecs: 10,
	})
	fakeQueue.stats = queue.QueueStats{
		ReadyDepth: 5,
		InFlight:   1,
	}
	loop.targetWorkers.Store(1)
	loop.scaleCooldown = time.Now().Add(30 * time.Second)

	next := loop.evaluateScaling(context.Background(), 1)
	if next != 1 {
		t.Fatalf("expected cooldown to hold, got %d", next)
	}
}

func newLoopForTest(t *testing.T, q *stubQueue, h *stubHandler, locker RunLocker, cfg Config) *Loop {
	t.Helper()
	logger := slog.Default()
	loop, err := NewLoop(q, h, locker, cfg, logger, nil)
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	return loop
}

type stubQueue struct {
	mu              sync.Mutex
	lease           *queue.JobLease
	leased          bool
	heartbeatErrors []error
	heartbeatCount  int
	ackCount        int
	nackCount       int
	nackDelays      []int
	stats           queue.QueueStats
}

type cancelLeaseQueue struct {
	started     chan struct{}
	startedOnce sync.Once
}

func (q *cancelLeaseQueue) EnqueueRun(
	_ context.Context,
	_ uuid.UUID,
	_ uuid.UUID,
	_ string,
	_ string,
	_ map[string]any,
	_ *time.Time,
) (uuid.UUID, error) {
	return uuid.New(), nil
}

func (q *cancelLeaseQueue) Lease(ctx context.Context, _ int, _ []string) (*queue.JobLease, error) {
	q.startedOnce.Do(func() {
		if q.started != nil {
			close(q.started)
		}
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

func (q *cancelLeaseQueue) Heartbeat(_ context.Context, _ queue.JobLease, _ int) error { return nil }
func (q *cancelLeaseQueue) Ack(_ context.Context, _ queue.JobLease) error              { return nil }
func (q *cancelLeaseQueue) Nack(_ context.Context, _ queue.JobLease, _ *int) error     { return nil }
func (q *cancelLeaseQueue) QueueDepth(_ context.Context, _ []string) (int, error)      { return 0, nil }

func (s *stubQueue) EnqueueRun(
	_ context.Context,
	_ uuid.UUID,
	_ uuid.UUID,
	_ string,
	_ string,
	_ map[string]any,
	_ *time.Time,
) (uuid.UUID, error) {
	return uuid.New(), nil
}

func (s *stubQueue) Lease(_ context.Context, _ int, _ []string) (*queue.JobLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leased {
		return nil, nil
	}
	s.leased = true
	if s.lease == nil {
		return nil, nil
	}
	copied := *s.lease
	return &copied, nil
}

func (s *stubQueue) Heartbeat(_ context.Context, _ queue.JobLease, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatCount++
	if len(s.heartbeatErrors) == 0 {
		return nil
	}
	err := s.heartbeatErrors[0]
	s.heartbeatErrors = s.heartbeatErrors[1:]
	return err
}

func (s *stubQueue) Ack(_ context.Context, _ queue.JobLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCount++
	return nil
}

func (s *stubQueue) Nack(_ context.Context, _ queue.JobLease, delay *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nackCount++
	chosen := -1
	if delay != nil {
		chosen = *delay
	}
	s.nackDelays = append(s.nackDelays, chosen)
	return nil
}

func (s *stubQueue) QueueDepth(_ context.Context, _ []string) (int, error) { return 0, nil }

func (s *stubQueue) QueueStats(_ context.Context, _ []string) (queue.QueueStats, error) {
	return s.stats, nil
}

type stubHandler struct {
	called           int
	blockUntilCancel bool
	// 非 nil 时 handler 在 ctx cancel 后等待此 channel 关闭才退出
	exitGate chan struct{}
}

func (s *stubHandler) Handle(ctx context.Context, _ queue.JobLease) error {
	s.called++
	if !s.blockUntilCancel {
		return nil
	}
	<-ctx.Done()
	if s.exitGate != nil {
		<-s.exitGate
	}
	return ctx.Err()
}

type stubLocker struct {
	acquired    bool
	unlockCalls int
	mu          sync.Mutex
}

func (s *stubLocker) TryAcquire(_ context.Context, _ uuid.UUID) (UnlockFunc, bool, error) {
	if !s.acquired {
		return nil, false, nil
	}
	return func(context.Context) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.unlockCalls++
		return nil
	}, true, nil
}

func (s *stubLocker) getUnlockCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unlockCalls
}
