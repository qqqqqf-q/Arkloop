//go:build desktop

package desktoprun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/desktop"
	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type lifecycleQueueStub struct {
	calls []lifecycleQueueCall
}

type lifecycleQueueCall struct {
	accountID uuid.UUID
	runID     uuid.UUID
	traceID   string
	jobType   string
	payload   map[string]any
}

func (s *lifecycleQueueStub) EnqueueRun(
	_ context.Context,
	accountID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	_ *time.Time,
) (uuid.UUID, error) {
	s.calls = append(s.calls, lifecycleQueueCall{
		accountID: accountID,
		runID:     runID,
		traceID:   traceID,
		jobType:   queueJobType,
		payload:   payload,
	})
	return uuid.New(), nil
}

func (s *lifecycleQueueStub) Lease(context.Context, int, []string) (*queue.JobLease, error) {
	return nil, nil
}

func (s *lifecycleQueueStub) Heartbeat(context.Context, queue.JobLease, int) error {
	return nil
}

func (s *lifecycleQueueStub) Ack(context.Context, queue.JobLease) error {
	return nil
}

func (s *lifecycleQueueStub) Nack(context.Context, queue.JobLease, *int) error {
	return nil
}

func (s *lifecycleQueueStub) QueueDepth(context.Context, []string) (int, error) {
	return 0, nil
}

func TestLifecycleBootstrapRecoversRecentRun(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	accountID, _, _, runID := seedLifecycleRun(t, ctx, db)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "llm.turn.completed",
		OccurredAt: time.Now().UTC().Add(-10 * time.Second),
		DataJSON: map[string]any{
			"trace_id": "trace-recover",
		},
	})
	seedRolloutMaterial(t, runID)

	q := &lifecycleQueueStub{}
	manager := newLifecycleManager(db, q, nil, nil)
	if err := manager.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	if len(q.calls) != 1 {
		t.Fatalf("expected 1 recovered run, got %d", len(q.calls))
	}
	call := q.calls[0]
	if call.accountID != accountID || call.runID != runID {
		t.Fatalf("unexpected recovery target: account=%s run=%s", call.accountID, call.runID)
	}
	if call.jobType != queue.RunExecuteJobType {
		t.Fatalf("unexpected recovery job type: %s", call.jobType)
	}
	if call.traceID != "trace-recover" {
		t.Fatalf("unexpected recovery trace id: %q", call.traceID)
	}
	if got, _ := call.payload["source"].(string); got != "desktop_recovery" {
		t.Fatalf("unexpected recovery payload: %#v", call.payload)
	}
}

func TestLifecycleBootstrapSkipsRunWithoutRecoveryMaterial(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	_, _, _, runID := seedLifecycleRun(t, ctx, db)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "llm.turn.completed",
		OccurredAt: time.Now().UTC().Add(-10 * time.Second),
		DataJSON: map[string]any{
			"trace_id": "trace-recover",
		},
	})

	q := &lifecycleQueueStub{}
	manager := newLifecycleManager(db, q, nil, nil)
	if err := manager.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	if len(q.calls) != 0 {
		t.Fatalf("expected no recovered runs without runtime state, got %d", len(q.calls))
	}
}

func TestLifecycleReapStaleRunUsesTwoStages(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	_, _, _, runID := seedLifecycleRun(t, ctx, db)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "llm.turn.completed",
		OccurredAt: time.Now().UTC().Add(-10 * time.Minute),
		DataJSON: map[string]any{
			"trace_id": "trace-stale",
		},
	})

	q := &lifecycleQueueStub{}
	manager := newLifecycleManager(db, q, nil, nil)
	if err := manager.reapOnce(ctx); err != nil {
		t.Fatalf("first reap failed: %v", err)
	}
	if len(q.calls) != 0 {
		t.Fatalf("expected stale run not to recover, got %d queued runs", len(q.calls))
	}

	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query run status: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected run to remain running after cancel request, got %q", status)
	}

	var eventType string
	if err := db.QueryRow(ctx,
		`SELECT type FROM run_events WHERE run_id = $1 ORDER BY seq DESC LIMIT 1`,
		runID,
	).Scan(&eventType); err != nil {
		t.Fatalf("query latest run event after first reap: %v", err)
	}
	if eventType != "run.cancel_requested" {
		t.Fatalf("expected latest event run.cancel_requested, got %q", eventType)
	}

	if _, err := db.Exec(ctx,
		`UPDATE run_events SET ts = $2 WHERE run_id = $1 AND type = 'run.cancel_requested'`,
		runID,
		time.Now().UTC().Add(-(desktopStaleCancelGrace + time.Second)),
	); err != nil {
		t.Fatalf("age cancel_requested event: %v", err)
	}

	if err := manager.reapOnce(ctx); err != nil {
		t.Fatalf("second reap failed: %v", err)
	}

	if err := db.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query run status after second reap: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected failed status after grace window, got %q", status)
	}

	var errorClass string
	if err := db.QueryRow(ctx,
		`SELECT type, COALESCE(error_class, '') FROM run_events WHERE run_id = $1 ORDER BY seq DESC LIMIT 1`,
		runID,
	).Scan(&eventType, &errorClass); err != nil {
		t.Fatalf("query latest run event after second reap: %v", err)
	}
	if eventType != "run.failed" {
		t.Fatalf("expected latest event run.failed, got %q", eventType)
	}
	if errorClass != "worker.timeout" {
		t.Fatalf("expected worker.timeout, got %q", errorClass)
	}
}

func TestLifecycleReapCanceledRunEvenIfInputAfter(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	_, _, _, runID := seedLifecycleRun(t, ctx, db)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "run.cancel_requested",
		OccurredAt: time.Now().UTC().Add(-(desktopStaleCancelGrace + time.Minute)),
	})
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "run.input_provided",
		OccurredAt: time.Now().UTC(),
		DataJSON: map[string]any{
			"content": "still waiting",
		},
	})

	q := &lifecycleQueueStub{}
	manager := newLifecycleManager(db, q, nil, nil)
	if err := manager.reapOnce(ctx); err != nil {
		t.Fatalf("reap failed: %v", err)
	}

	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query run status after reap: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected failed status, got %q", status)
	}

	var eventType string
	if err := db.QueryRow(ctx,
		`SELECT type FROM run_events WHERE run_id = $1 ORDER BY seq DESC LIMIT 1`,
		runID,
	).Scan(&eventType); err != nil {
		t.Fatalf("query latest event after reap: %v", err)
	}
	if eventType != "run.failed" {
		t.Fatalf("expected run.failed event, got %q", eventType)
	}
}

func TestLifecycleRecoverSkipsRunWithHistoricalCancelRequest(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	_, _, _, runID := seedLifecycleRun(t, ctx, db)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "run.cancel_requested",
		OccurredAt: time.Now().UTC().Add(-2 * time.Minute),
		DataJSON: map[string]any{
			"trace_id": "trace-cancel",
		},
	})
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "run.input_provided",
		OccurredAt: time.Now().UTC().Add(-90 * time.Second),
		DataJSON: map[string]any{
			"content":  "late input",
			"trace_id": "trace-input",
		},
	})

	q := &lifecycleQueueStub{}
	manager := newLifecycleManager(db, q, nil, nil)
	if err := manager.recoverRuns(ctx); err != nil {
		t.Fatalf("recover runs failed: %v", err)
	}
	if len(q.calls) != 0 {
		t.Fatalf("expected run with historical cancel request not to recover, got %d queued runs", len(q.calls))
	}
}

func TestLifecycleBootstrapSyncsTerminalStatusFromEvents(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	_, _, _, runID := seedLifecycleRun(t, ctx, db)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "run.completed",
		OccurredAt: time.Now().UTC().Add(-30 * time.Second),
		DataJSON: map[string]any{
			"trace_id": "trace-terminal",
		},
	})

	q := &lifecycleQueueStub{}
	manager := newLifecycleManager(db, q, nil, nil)
	if err := manager.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if len(q.calls) != 0 {
		t.Fatalf("expected terminal run not to recover, got %d queued runs", len(q.calls))
	}

	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query run status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("expected completed status, got %q", status)
	}
}

func openLifecycleTestDB(t *testing.T, ctx context.Context) (data.DesktopDB, func()) {
	t.Helper()

	dataDir := filepath.Join(t.TempDir(), "desktop-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	cleanup := func() {
		_ = sqlitePool.Close()
	}
	return sqlitepgx.New(sqlitePool.Unwrap()), cleanup
}

func seedLifecycleRun(t *testing.T, ctx context.Context, db data.DesktopDB) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID, "desktop-lifecycle-" + accountID.String(), "Desktop Lifecycle"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID, accountID, "Lifecycle Project"},
		},
		{
			sql:  `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
			args: []any{threadID, accountID, projectID},
		},
		{
			sql:  `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`,
			args: []any{runID, accountID, threadID},
		},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed lifecycle data: %v", err)
		}
	}
	return accountID, projectID, threadID, runID
}

func appendLifecycleEvent(t *testing.T, ctx context.Context, db data.DesktopDB, runID uuid.UUID, ev events.RunEvent) {
	t.Helper()

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin lifecycle event tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := (data.DesktopRunEventsRepository{}).AppendRunEvent(ctx, tx, runID, ev); err != nil {
		t.Fatalf("append lifecycle event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lifecycle event: %v", err)
	}
}

func seedRolloutMaterial(t *testing.T, runID uuid.UUID) {
	t.Helper()

	ctx := context.Background()
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		t.Fatalf("resolve desktop data dir: %v", err)
	}

	opener := objectstore.NewFilesystemOpener(desktop.StorageRoot(dataDir))
	store, err := opener.Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatalf("rollout store does not implement blob store")
	}
	key := fmt.Sprintf("run/%s.jsonl", runID.String())
	if err := blobStore.Put(ctx, key, []byte("{}\n")); err != nil {
		t.Fatalf("put rollout material: %v", err)
	}
}
