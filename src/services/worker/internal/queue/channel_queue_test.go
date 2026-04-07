package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newChannelQueue(t *testing.T, maxAttempts int) *ChannelJobQueue {
	t.Helper()
	q, err := NewChannelJobQueue(maxAttempts, nil)
	if err != nil {
		t.Fatalf("NewChannelJobQueue failed: %v", err)
	}
	return q
}

func newChannelQueueWithCallback(t *testing.T, maxAttempts int, onEnqueue func()) *ChannelJobQueue {
	t.Helper()
	q, err := NewChannelJobQueue(maxAttempts, onEnqueue)
	if err != nil {
		t.Fatalf("NewChannelJobQueue failed: %v", err)
	}
	return q
}

func isChannelLeaseLostError(err error) bool {
	if err == nil {
		return false
	}
	var target *LeaseLostError
	return errors.As(err, &target)
}

func TestChannelQueueBasicEnqueueAndLease(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	jobID, err := q.EnqueueRun(ctx, accountID, runID, traceID, RunExecuteJobType, map[string]any{"source": "test"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if jobID == uuid.Nil {
		t.Fatal("expected non-nil job ID")
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	if lease.JobID != jobID {
		t.Fatalf("expected job ID %s, got %s", jobID, lease.JobID)
	}
	if lease.JobType != RunExecuteJobType {
		t.Fatalf("expected job type %s, got %s", RunExecuteJobType, lease.JobType)
	}

	// Verify payload envelope fields
	if lease.PayloadJSON["job_id"] != jobID.String() {
		t.Fatalf("expected payload job_id %s, got %v", jobID.String(), lease.PayloadJSON["job_id"])
	}
	if lease.PayloadJSON["v"] != float64(1) {
		t.Fatalf("expected payload v float64(1), got %v (%T)", lease.PayloadJSON["v"], lease.PayloadJSON["v"])
	}
	if lease.PayloadJSON["run_id"] != runID.String() {
		t.Fatalf("expected payload run_id %s, got %v", runID.String(), lease.PayloadJSON["run_id"])
	}
	if lease.PayloadJSON["account_id"] != accountID.String() {
		t.Fatalf("expected payload account_id %s, got %v", accountID.String(), lease.PayloadJSON["account_id"])
	}
	if lease.PayloadJSON["trace_id"] != traceID {
		t.Fatalf("expected payload trace_id %s, got %v", traceID, lease.PayloadJSON["trace_id"])
	}

	inner, ok := lease.PayloadJSON["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload.payload to be map[string]any, got %T", lease.PayloadJSON["payload"])
	}
	if inner["source"] != "test" {
		t.Fatalf("expected payload.payload.source 'test', got %v", inner["source"])
	}
}

func TestChannelQueueLeaseIsMutuallyExclusive(t *testing.T) {
	q := newChannelQueue(t, 25)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	jobID, err := q.EnqueueRun(ctx, accountID, runID, traceID, RunExecuteJobType, map[string]any{"source": "test"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	start := make(chan struct{})
	leaseCh := make(chan *JobLease, 2)
	errCh := make(chan error, 2)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
			if err != nil {
				errCh <- err
				return
			}
			leaseCh <- lease
		}()
	}
	close(start)
	wg.Wait()
	close(leaseCh)
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("lease returned error: %v", err)
		}
	}

	count := 0
	for lease := range leaseCh {
		if lease == nil {
			continue
		}
		count++
		if lease.JobID != jobID {
			t.Fatalf("unexpected leased job id: %s", lease.JobID)
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one lease, got %d", count)
	}
}

func TestChannelQueuePayloadIsCompatible(t *testing.T) {
	q := newChannelQueue(t, 25)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	jobID, err := q.EnqueueRun(ctx, accountID, runID, traceID, RunExecuteJobType, map[string]any{"note": "compat"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	if lease.JobID != jobID {
		t.Fatalf("unexpected job id: %s", lease.JobID)
	}
	if lease.JobType != RunExecuteJobType {
		t.Fatalf("unexpected job type: %s", lease.JobType)
	}
	if lease.PayloadJSON["v"] != float64(JobPayloadVersionV1) {
		t.Fatalf("unexpected payload version: %v (%T)", lease.PayloadJSON["v"], lease.PayloadJSON["v"])
	}
	if lease.PayloadJSON["run_id"] != runID.String() {
		t.Fatalf("unexpected run_id in payload: %v", lease.PayloadJSON["run_id"])
	}
	if lease.PayloadJSON["account_id"] != accountID.String() {
		t.Fatalf("unexpected account_id in payload: %v", lease.PayloadJSON["account_id"])
	}
	if lease.PayloadJSON["trace_id"] != traceID {
		t.Fatalf("unexpected trace_id in payload: %v", lease.PayloadJSON["trace_id"])
	}
}

func TestChannelQueueDeadLettersAfterMaxAttempts(t *testing.T) {
	q := newChannelQueue(t, 2)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	_, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"note": "dead_letter"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Attempt 1: lease and nack
	lease1, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease #1 failed: %v", err)
	}
	if lease1 == nil || lease1.Attempts != 1 {
		t.Fatalf("unexpected lease #1: %+v", lease1)
	}
	zero := 0
	if err := q.Nack(ctx, *lease1, &zero); err != nil {
		t.Fatalf("nack #1 failed: %v", err)
	}

	// Attempt 2: lease and nack (should dead-letter because attempts == maxAttempts)
	lease2, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease #2 failed: %v", err)
	}
	if lease2 == nil || lease2.Attempts != 2 {
		t.Fatalf("unexpected lease #2: %+v", lease2)
	}
	if err := q.Nack(ctx, *lease2, &zero); err != nil {
		t.Fatalf("nack #2 failed: %v", err)
	}

	// Attempt 3: should return nil (job is dead)
	lease3, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease #3 failed: %v", err)
	}
	if lease3 != nil {
		t.Fatalf("expected nil lease after dead-letter, got %+v", lease3)
	}

	// Verify internal state
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, job := range q.jobs {
		if job.status != JobStatusDead {
			t.Fatalf("expected job status %s, got %s", JobStatusDead, job.status)
		}
		if job.attempts != 2 {
			t.Fatalf("expected 2 attempts, got %d", job.attempts)
		}
	}
}

func TestChannelQueueLeaseCanFilterByJobType(t *testing.T) {
	q := newChannelQueue(t, 25)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()
	otherJobType := "run.execute.test_other"

	pythonJobID, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"note": "python"}, nil)
	if err != nil {
		t.Fatalf("enqueue python job failed: %v", err)
	}

	goJobID, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", otherJobType, map[string]any{"note": "other"}, nil)
	if err != nil {
		t.Fatalf("enqueue go job failed: %v", err)
	}

	// Lease only the other type
	leaseGo, err := q.Lease(ctx, 60, []string{otherJobType})
	if err != nil {
		t.Fatalf("lease go job failed: %v", err)
	}
	if leaseGo == nil {
		t.Fatal("expected go lease but got nil")
	}
	if leaseGo.JobID != goJobID || leaseGo.JobType != otherJobType {
		t.Fatalf("unexpected go lease: %+v", leaseGo)
	}
	if leaseGo.PayloadJSON["type"] != otherJobType {
		t.Fatalf("unexpected payload type for other job: %#v", leaseGo.PayloadJSON["type"])
	}
	if err := q.Ack(ctx, *leaseGo); err != nil {
		t.Fatalf("ack go job failed: %v", err)
	}

	// Lease only the python type
	leasePython, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease python job failed: %v", err)
	}
	if leasePython == nil {
		t.Fatal("expected python lease but got nil")
	}
	if leasePython.JobID != pythonJobID || leasePython.JobType != RunExecuteJobType {
		t.Fatalf("unexpected python lease: %+v", leasePython)
	}
	if leasePython.PayloadJSON["type"] != RunExecuteJobType {
		t.Fatalf("unexpected payload type for run.execute job: %#v", leasePython.PayloadJSON["type"])
	}
	if err := q.Ack(ctx, *leasePython); err != nil {
		t.Fatalf("ack python job failed: %v", err)
	}
}

func TestChannelQueueRejectsLeaseTokenMismatch(t *testing.T) {
	q := newChannelQueue(t, 25)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	_, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, nil, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	tampered := *lease
	tampered.LeaseToken = uuid.New()

	if err := q.Ack(ctx, tampered); !isChannelLeaseLostError(err) {
		t.Fatalf("ack mismatch expected LeaseLostError, got %v", err)
	}
	if err := q.Heartbeat(ctx, tampered, 30); !isChannelLeaseLostError(err) {
		t.Fatalf("heartbeat mismatch expected LeaseLostError, got %v", err)
	}
	if err := q.Nack(ctx, tampered, nil); !isChannelLeaseLostError(err) {
		t.Fatalf("nack mismatch expected LeaseLostError, got %v", err)
	}

	// Original token should still work
	if err := q.Ack(ctx, *lease); err != nil {
		t.Fatalf("ack original lease failed: %v", err)
	}
}

func TestChannelQueueAckRemovesJob(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	_, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"note": "ack"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	if err := q.Ack(ctx, *lease); err != nil {
		t.Fatalf("ack failed: %v", err)
	}

	// Lease again should return nil (job is done)
	lease2, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("second lease failed: %v", err)
	}
	if lease2 != nil {
		t.Fatalf("expected nil lease after ack, got %+v", lease2)
	}
}

func TestChannelQueueNackRequeuesJob(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	_, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"note": "nack"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}
	if lease.Attempts != 1 {
		t.Fatalf("expected attempts 1, got %d", lease.Attempts)
	}

	zero := 0
	if err := q.Nack(ctx, *lease, &zero); err != nil {
		t.Fatalf("nack failed: %v", err)
	}

	// Lease again should succeed with incremented attempts
	lease2, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("second lease failed: %v", err)
	}
	if lease2 == nil {
		t.Fatal("expected lease after nack but got nil")
	}
	if lease2.Attempts != 2 {
		t.Fatalf("expected attempts 2, got %d", lease2.Attempts)
	}
}

func TestChannelQueueHeartbeatExtendsLease(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	_, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"note": "heartbeat"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	lease, err := q.Lease(ctx, 5, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	originalLeasedUntil := lease.LeasedUntil

	// Heartbeat with a longer lease
	if err := q.Heartbeat(ctx, *lease, 120); err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	// Verify the internal lease time was extended
	q.mu.Lock()
	job := q.jobs[lease.JobID]
	newLeasedUntil := job.leasedUntil
	q.mu.Unlock()

	if !newLeasedUntil.After(originalLeasedUntil) {
		t.Fatalf("expected lease to be extended, original=%v new=%v", originalLeasedUntil, newLeasedUntil)
	}
}

func TestChannelQueueDelayedJob(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	futureTime := time.Now().Add(1 * time.Hour)
	_, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"note": "delayed"}, &futureTime)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Lease should return nil because the job is not yet available
	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease != nil {
		t.Fatalf("expected nil lease for delayed job, got %+v", lease)
	}
}

func TestChannelQueueExpiredLeaseNotReclaimed(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	_, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"note": "expired"}, nil)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	// Simulate lease expiration by manipulating internal state
	q.mu.Lock()
	job := q.jobs[lease.JobID]
	job.leasedUntil = time.Now().Add(-1 * time.Second)
	q.mu.Unlock()

	// Expired leased job should NOT be re-leased in single-process mode
	lease2, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("second lease call failed: %v", err)
	}
	if lease2 != nil {
		t.Fatal("expected nil lease for expired leased job, but got a lease")
	}
}

func TestChannelQueueOnEnqueueCallback(t *testing.T) {
	var callCount atomic.Int32

	q := newChannelQueueWithCallback(t, 5, func() {
		callCount.Add(1)
	})
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	for i := 0; i < 3; i++ {
		jobID, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"i": i}, nil)
		if i == 0 && err != nil {
			t.Fatalf("enqueue #%d failed: %v", i, err)
		}
		if i == 0 && jobID == uuid.Nil {
			t.Fatalf("enqueue #%d returned nil job id", i)
		}
		if i > 0 && !errors.Is(err, ErrRunExecuteAlreadyQueued) {
			t.Fatalf("enqueue #%d expected ErrRunExecuteAlreadyQueued, got %v", i, err)
		}
	}

	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected onEnqueue to be called 1 time, got %d", got)
	}
}

func TestChannelQueueRejectsDuplicateRunExecute(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	jobID1, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"i": 1}, nil)
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	_, err = q.EnqueueRun(ctx, accountID, runID, "fedcba9876543210fedcba9876543210", RunExecuteJobType, map[string]any{"i": 2}, nil)
	if !errors.Is(err, ErrRunExecuteAlreadyQueued) {
		t.Fatalf("expected ErrRunExecuteAlreadyQueued, got %v", err)
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}
	if lease.JobID != jobID1 {
		t.Fatalf("expected original job id %s, got %s", jobID1, lease.JobID)
	}
}

func TestChannelQueueEmptyLeaseReturnsNil(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease on empty queue failed: %v", err)
	}
	if lease != nil {
		t.Fatalf("expected nil lease on empty queue, got %+v", lease)
	}
}

func TestChannelQueueNilPayload(t *testing.T) {
	q := newChannelQueue(t, 5)
	ctx := context.Background()

	accountID := uuid.New()
	runID := uuid.New()

	jobID, err := q.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, nil, nil)
	if err != nil {
		t.Fatalf("enqueue with nil payload failed: %v", err)
	}
	if jobID == uuid.Nil {
		t.Fatal("expected non-nil job ID")
	}

	lease, err := q.Lease(ctx, 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	// The inner payload should be an empty map (from nil -> copy loop -> empty map)
	inner, ok := lease.PayloadJSON["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload.payload to be map[string]any, got %T", lease.PayloadJSON["payload"])
	}
	if len(inner) != 0 {
		t.Fatalf("expected empty inner payload, got %v", inner)
	}
}
