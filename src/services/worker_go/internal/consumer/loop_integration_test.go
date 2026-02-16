package consumer

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker_go/internal/app"
	"arkloop/services/worker_go/internal/queue"
	"arkloop/services/worker_go/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLoopDedupesDuplicateRunJobsViaAdvisoryLock(t *testing.T) {
	fixture := newIntegrationFixture(t)

	orgID := uuid.New()
	runID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	if _, err := fixture.queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		traceID,
		queue.RunExecuteJobType,
		nil,
		nil,
	); err != nil {
		t.Fatalf("enqueue #1 failed: %v", err)
	}
	if _, err := fixture.queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		traceID,
		queue.RunExecuteJobType,
		nil,
		nil,
	); err != nil {
		t.Fatalf("enqueue #2 failed: %v", err)
	}

	handler := &blockingHandler{started: make(chan struct{}), release: make(chan struct{})}
	cfg := Config{
		Concurrency:      1,
		PollSeconds:      0,
		LeaseSeconds:     30,
		HeartbeatSeconds: 0,
		QueueJobTypes:    []string{queue.RunExecuteJobType},
	}
	logger := app.NewJSONLogger("worker_go_test", nil)

	loop1, err := NewLoop(fixture.queue, handler, fixture.locker, cfg, logger)
	if err != nil {
		t.Fatalf("NewLoop #1 failed: %v", err)
	}
	loop2, err := NewLoop(fixture.queue, handler, fixture.locker, cfg, logger)
	if err != nil {
		t.Fatalf("NewLoop #2 failed: %v", err)
	}

	resultCh := make(chan bool, 1)
	errCh := make(chan error, 1)
	go func() {
		processed, err := loop1.RunOnce(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- processed
	}()

	select {
	case <-handler.started:
	case <-time.After(2 * time.Second):
		t.Fatal("loop1 handler did not start")
	}

	processed2, err := loop2.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("loop2 RunOnce failed: %v", err)
	}
	if !processed2 {
		t.Fatal("loop2 should process one leased job")
	}

	close(handler.release)

	select {
	case err := <-errCh:
		t.Fatalf("loop1 returned error: %v", err)
	case processed1 := <-resultCh:
		if !processed1 {
			t.Fatal("loop1 should process one leased job")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("loop1 did not finish")
	}

	statuses := readStatusesByRunID(t, fixture.pool, runID)
	sort.Strings(statuses)
	if len(statuses) != 2 {
		t.Fatalf("unexpected status count: %+v", statuses)
	}
	if statuses[0] != queue.JobStatusDone || statuses[1] != queue.JobStatusQueued {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}
	if handler.calls() != 1 {
		t.Fatalf("handler should be called once, got %d", handler.calls())
	}
}

func TestLoopNacksWhenHeartbeatFailsRepeatedly(t *testing.T) {
	fixture := newIntegrationFixture(t)

	orgID := uuid.New()
	runID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	jobID, err := fixture.queue.EnqueueRun(
		context.Background(),
		orgID,
		runID,
		traceID,
		queue.RunExecuteJobType,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	handler := &blockingUntilCancelledHandler{}
	flakyQueue := &flakyHeartbeatQueue{base: fixture.queue, failuresLeft: 3}
	cfg := Config{
		Concurrency:      1,
		PollSeconds:      0,
		LeaseSeconds:     30,
		HeartbeatSeconds: 0.01,
		QueueJobTypes:    []string{queue.RunExecuteJobType},
	}
	logger := app.NewJSONLogger("worker_go_test", nil)

	loop, err := NewLoop(flakyQueue, handler, fixture.locker, cfg, logger)
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}

	processed, err := loop.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if !processed {
		t.Fatal("RunOnce should process one leased job")
	}

	status, attempts := readJobStatus(t, fixture.pool, jobID)
	if status != queue.JobStatusQueued {
		t.Fatalf("unexpected status: %s", status)
	}
	if attempts != 1 {
		t.Fatalf("unexpected attempts: %d", attempts)
	}
}

type integrationFixture struct {
	pool   *pgxpool.Pool
	queue  *queue.PgQueue
	locker *PgAdvisoryLocker
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "arkloop_wg03")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	queueClient, err := queue.NewPgQueue(pool, 25)
	if err != nil {
		t.Fatalf("NewPgQueue failed: %v", err)
	}
	locker, err := NewPgAdvisoryLocker(pool)
	if err != nil {
		t.Fatalf("NewPgAdvisoryLocker failed: %v", err)
	}

	return &integrationFixture{
		pool:   pool,
		queue:  queueClient,
		locker: locker,
	}
}

type blockingHandler struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	count   int
}

func (h *blockingHandler) Handle(_ context.Context, _ queue.JobLease) error {
	h.once.Do(func() { close(h.started) })
	h.mu.Lock()
	h.count++
	h.mu.Unlock()
	<-h.release
	return nil
}

func (h *blockingHandler) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

type blockingUntilCancelledHandler struct{}

func (blockingUntilCancelledHandler) Handle(ctx context.Context, _ queue.JobLease) error {
	<-ctx.Done()
	return ctx.Err()
}

type flakyHeartbeatQueue struct {
	base         queue.JobQueue
	failuresLeft int
	mu           sync.Mutex
}

func (q *flakyHeartbeatQueue) EnqueueRun(
	ctx context.Context,
	orgID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	availableAt *time.Time,
) (uuid.UUID, error) {
	return q.base.EnqueueRun(ctx, orgID, runID, traceID, queueJobType, payload, availableAt)
}

func (q *flakyHeartbeatQueue) Lease(ctx context.Context, leaseSeconds int, jobTypes []string) (*queue.JobLease, error) {
	return q.base.Lease(ctx, leaseSeconds, jobTypes)
}

func (q *flakyHeartbeatQueue) Heartbeat(ctx context.Context, lease queue.JobLease, leaseSeconds int) error {
	q.mu.Lock()
	if q.failuresLeft > 0 {
		q.failuresLeft--
		q.mu.Unlock()
		return errors.New("heartbeat failed")
	}
	q.mu.Unlock()
	return q.base.Heartbeat(ctx, lease, leaseSeconds)
}

func (q *flakyHeartbeatQueue) Ack(ctx context.Context, lease queue.JobLease) error {
	return q.base.Ack(ctx, lease)
}

func (q *flakyHeartbeatQueue) Nack(ctx context.Context, lease queue.JobLease, delay *int) error {
	return q.base.Nack(ctx, lease, delay)
}

func readStatusesByRunID(t *testing.T, pool *pgxpool.Pool, runID uuid.UUID) []string {
	t.Helper()

	rows, err := pool.Query(
		context.Background(),
		`SELECT status FROM jobs WHERE payload_json->>'run_id' = $1 ORDER BY created_at ASC, id ASC`,
		runID.String(),
	)
	if err != nil {
		t.Fatalf("query statuses failed: %v", err)
	}
	defer rows.Close()

	statuses := make([]string, 0, 2)
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan status failed: %v", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	return statuses
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
