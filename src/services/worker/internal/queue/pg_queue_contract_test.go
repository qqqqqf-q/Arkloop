package queue

import (
	"context"
	"errors"
	"sync"
	"testing"

	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPgQueueLeaseIsMutuallyExclusive(t *testing.T) {
	fixture := newQueueFixture(t, 25)
	queue := fixture.queue

	orgID := uuid.New()
	runID := uuid.New()
	traceID := uuid.NewString()
	traceID = traceID[:8] + traceID[9:13] + traceID[14:18] + traceID[19:23] + traceID[24:]

	jobID, err := queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		traceID,
		RunExecuteJobType,
		map[string]any{"source": "test"},
		nil,
	)
	if err != nil {
		t.Fatalf("enqueue run failed: %v", err)
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
			lease, err := queue.Lease(context.Background(), 60, []string{RunExecuteJobType})
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

func TestPgQueuePayloadIsCompatible(t *testing.T) {
	fixture := newQueueFixture(t, 25)
	queue := fixture.queue

	orgID := uuid.New()
	runID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	jobID, err := queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		traceID,
		RunExecuteJobType,
		map[string]any{"note": "compat"},
		nil,
	)
	if err != nil {
		t.Fatalf("enqueue run failed: %v", err)
	}

	lease, err := queue.Lease(context.Background(), 60, []string{RunExecuteJobType})
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
		t.Fatalf("unexpected payload version: %v", lease.PayloadJSON["v"])
	}
	if lease.PayloadJSON["run_id"] != runID.String() {
		t.Fatalf("unexpected run id in payload: %v", lease.PayloadJSON["run_id"])
	}
	if lease.PayloadJSON["org_id"] != orgID.String() {
		t.Fatalf("unexpected org id in payload: %v", lease.PayloadJSON["org_id"])
	}
	if lease.PayloadJSON["trace_id"] != traceID {
		t.Fatalf("unexpected trace_id in payload: %v", lease.PayloadJSON["trace_id"])
	}
}

func TestPgQueueDeadLettersAfterMaxAttempts(t *testing.T) {
	fixture := newQueueFixture(t, 2)
	queue := fixture.queue

	orgID := uuid.New()
	runID := uuid.New()

	jobID, err := queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		"0123456789abcdef0123456789abcdef",
		RunExecuteJobType,
		map[string]any{"note": "dead_letter"},
		nil,
	)
	if err != nil {
		t.Fatalf("enqueue run failed: %v", err)
	}

	lease1, err := queue.Lease(context.Background(), 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease #1 failed: %v", err)
	}
	if lease1 == nil || lease1.Attempts != 1 {
		t.Fatalf("unexpected lease #1: %+v", lease1)
	}
	zero := 0
	if err := queue.Nack(context.Background(), *lease1, &zero); err != nil {
		t.Fatalf("nack #1 failed: %v", err)
	}

	lease2, err := queue.Lease(context.Background(), 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease #2 failed: %v", err)
	}
	if lease2 == nil || lease2.Attempts != 2 {
		t.Fatalf("unexpected lease #2: %+v", lease2)
	}
	if err := queue.Nack(context.Background(), *lease2, &zero); err != nil {
		t.Fatalf("nack #2 failed: %v", err)
	}

	lease3, err := queue.Lease(context.Background(), 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease #3 failed: %v", err)
	}
	if lease3 != nil {
		t.Fatalf("expected nil lease after dead-letter, got %+v", lease3)
	}

	status, attempts := readJobStatus(t, fixture.pool, jobID)
	if status != JobStatusDead {
		t.Fatalf("unexpected status: %s", status)
	}
	if attempts != 2 {
		t.Fatalf("unexpected attempts: %d", attempts)
	}
}

func TestPgQueueLeaseCanFilterByJobType(t *testing.T) {
	fixture := newQueueFixture(t, 25)
	queue := fixture.queue

	orgID := uuid.New()
	runID := uuid.New()
	otherJobType := "run.execute.test_other"

	pythonJobID, err := queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		"0123456789abcdef0123456789abcdef",
		RunExecuteJobType,
		map[string]any{"note": "python"},
		nil,
	)
	if err != nil {
		t.Fatalf("enqueue python job failed: %v", err)
	}

	goJobID, err := queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		"0123456789abcdef0123456789abcdef",
		otherJobType,
		map[string]any{"note": "other"},
		nil,
	)
	if err != nil {
		t.Fatalf("enqueue go job failed: %v", err)
	}

	leaseGo, err := queue.Lease(context.Background(), 60, []string{otherJobType})
	if err != nil {
		t.Fatalf("lease go job failed: %v", err)
	}
	if leaseGo == nil {
		t.Fatal("expected go lease but got nil")
	}
	if leaseGo.JobID != goJobID || leaseGo.JobType != otherJobType {
		t.Fatalf("unexpected go lease: %+v", leaseGo)
	}
	if err := queue.Ack(context.Background(), *leaseGo); err != nil {
		t.Fatalf("ack go job failed: %v", err)
	}

	leasePython, err := queue.Lease(context.Background(), 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease python job failed: %v", err)
	}
	if leasePython == nil {
		t.Fatal("expected python lease but got nil")
	}
	if leasePython.JobID != pythonJobID || leasePython.JobType != RunExecuteJobType {
		t.Fatalf("unexpected python lease: %+v", leasePython)
	}
	if err := queue.Ack(context.Background(), *leasePython); err != nil {
		t.Fatalf("ack python job failed: %v", err)
	}
}

func TestPgQueueRejectsLeaseTokenMismatch(t *testing.T) {
	fixture := newQueueFixture(t, 25)
	queue := fixture.queue

	orgID := uuid.New()
	runID := uuid.New()

	_, err := queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		"0123456789abcdef0123456789abcdef",
		RunExecuteJobType,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("enqueue run failed: %v", err)
	}

	lease, err := queue.Lease(context.Background(), 60, []string{RunExecuteJobType})
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease but got nil")
	}

	tampered := *lease
	tampered.LeaseToken = uuid.New()

	if err := queue.Ack(context.Background(), tampered); !isLeaseLostError(err) {
		t.Fatalf("ack mismatch expected LeaseLostError, got %v", err)
	}
	if err := queue.Heartbeat(context.Background(), tampered, 30); !isLeaseLostError(err) {
		t.Fatalf("heartbeat mismatch expected LeaseLostError, got %v", err)
	}
	if err := queue.Nack(context.Background(), tampered, nil); !isLeaseLostError(err) {
		t.Fatalf("nack mismatch expected LeaseLostError, got %v", err)
	}

	if err := queue.Ack(context.Background(), *lease); err != nil {
		t.Fatalf("ack original lease failed: %v", err)
	}
}

func isLeaseLostError(err error) bool {
	if err == nil {
		return false
	}
	var target *LeaseLostError
	return errors.As(err, &target)
}

type queueFixture struct {
	pool  *pgxpool.Pool
	queue *PgQueue
}

func newQueueFixture(t *testing.T, maxAttempts int) *queueFixture {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "arkloop_wg02")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	queue, err := NewPgQueue(pool, maxAttempts)
	if err != nil {
		t.Fatalf("NewPgQueue failed: %v", err)
	}

	return &queueFixture{pool: pool, queue: queue}
}

func readJobStatus(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) (string, int) {
	t.Helper()

	var (
		status   string
		attempts int
	)
	err := pool.QueryRow(
		context.Background(),
		`SELECT status, attempts FROM jobs WHERE id = $1`,
		jobID,
	).Scan(&status, &attempts)
	if err != nil {
		t.Fatalf("query job status failed: %v", err)
	}
	return status, attempts
}
