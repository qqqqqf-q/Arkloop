package consumer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker/internal/app"
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

		logger := app.NewJSONLogger("worker_go_test", nil)
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

func newLoopForTest(t *testing.T, q *stubQueue, h *stubHandler, locker RunLocker, cfg Config) *Loop {
	t.Helper()
	logger := app.NewJSONLogger("worker_go_test", nil)
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

type stubHandler struct {
	called           int
	blockUntilCancel bool
}

func (s *stubHandler) Handle(ctx context.Context, _ queue.JobLease) error {
	s.called++
	if !s.blockUntilCancel {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

type stubLocker struct {
	acquired bool
}

func (s *stubLocker) TryAcquire(_ context.Context, _ uuid.UUID) (UnlockFunc, bool, error) {
	if !s.acquired {
		return nil, false, nil
	}
	return func(context.Context) error { return nil }, true, nil
}
