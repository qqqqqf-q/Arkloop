//go:build desktop

package data

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestDesktopSubAgentRepository_CreateAndEvents(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	parentThreadID := uuid.New()
	childThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, parentThreadID)
	seedDesktopThread(t, pool, accountID, projectID, childThreadID)
	seedDesktopRun(t, pool, accountID, parentThreadID, parentRunID, nil)
	seedDesktopRun(t, pool, accountID, childThreadID, childRunID, &parentRunID)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	subAgentsRepo := SubAgentRepository{}
	record, err := subAgentsRepo.Create(ctx, tx, SubAgentCreateParams{
		AccountID:     accountID,
		OwnerThreadID: parentThreadID,
		AgentThreadID: childThreadID,
		OriginRunID:   parentRunID,
		Depth:         1,
		PersonaID:     ptr("normal"),
		SourceType:    SubAgentSourceTypeThreadSpawn,
		ContextMode:   SubAgentContextModeIsolated,
	})
	if err != nil {
		t.Fatalf("create sub_agent: %v", err)
	}
	if record.AccountID != accountID {
		t.Fatalf("unexpected account id: got %s want %s", record.AccountID, accountID)
	}

	if err := subAgentsRepo.TransitionToQueued(ctx, tx, record.ID, childRunID); err != nil {
		t.Fatalf("transition queued: %v", err)
	}
	if err := subAgentsRepo.TransitionToRunning(ctx, tx, childRunID); err != nil {
		t.Fatalf("transition running: %v", err)
	}

	eventsRepo := SubAgentEventsRepository{}
	firstSeq, err := eventsRepo.AppendEvent(ctx, tx, record.ID, nil, SubAgentEventTypeSpawnRequested, nil, nil)
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	secondSeq, err := eventsRepo.AppendEvent(ctx, tx, record.ID, &childRunID, SubAgentEventTypeRunQueued, map[string]any{
		"run_id": childRunID.String(),
	}, nil)
	if err != nil {
		t.Fatalf("append second event: %v", err)
	}
	if secondSeq <= firstSeq {
		t.Fatalf("expected increasing seq, got %d then %d", firstSeq, secondSeq)
	}

	pendingRepo := SubAgentPendingInputsRepository{}
	queuedInput, err := pendingRepo.Enqueue(ctx, tx, record.ID, "继续处理", true)
	if err != nil {
		t.Fatalf("enqueue pending input: %v", err)
	}
	if queuedInput.Seq != 1 {
		t.Fatalf("unexpected pending input seq: %d", queuedInput.Seq)
	}

	snapshotsRepo := SubAgentContextSnapshotsRepository{}
	if err := snapshotsRepo.Upsert(ctx, tx, record.ID, []byte(`{"messages":[]}`)); err != nil {
		t.Fatalf("upsert snapshot: %v", err)
	}

	if err := subAgentsRepo.TransitionToTerminal(ctx, tx, childRunID, SubAgentStatusCompleted, nil); err != nil {
		t.Fatalf("transition terminal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }() //nolint:errcheck

	got, err := subAgentsRepo.Get(ctx, readTx, record.ID)
	if err != nil {
		t.Fatalf("get sub_agent: %v", err)
	}
	if got == nil {
		t.Fatal("expected sub_agent record")
	}
	if got.Status != SubAgentStatusCompleted {
		t.Fatalf("unexpected terminal status: %s", got.Status)
	}
	if got.LastCompletedRunID == nil || *got.LastCompletedRunID != childRunID {
		t.Fatalf("unexpected last_completed_run_id: %#v", got.LastCompletedRunID)
	}

	events, err := eventsRepo.ListBySubAgent(ctx, readTx, record.ID, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	pendingItems, err := pendingRepo.ListBySubAgentForUpdate(ctx, readTx, record.ID)
	if err != nil {
		t.Fatalf("list pending inputs: %v", err)
	}
	if len(pendingItems) != 1 {
		t.Fatalf("expected 1 pending input, got %d", len(pendingItems))
	}

	snapshot, err := snapshotsRepo.GetBySubAgentID(ctx, readTx, record.ID)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if snapshot == nil || string(snapshot.SnapshotJSON) != `{"messages":[]}` {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestDesktopRunsRepository_TouchRunActivity(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)
	seedDesktopRun(t, pool, accountID, threadID, runID, nil)

	oldActivity := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC).Format("2006-01-02 15:04:05")
	if _, err := pool.Exec(ctx, `UPDATE runs SET status_updated_at = $2 WHERE id = $1`, runID, oldActivity); err != nil {
		t.Fatalf("set old activity: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	if err := (DesktopRunsRepository{}).TouchRunActivity(ctx, tx, runID); err != nil {
		t.Fatalf("touch run activity: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	var touched int
	if err := pool.QueryRow(
		ctx,
		`SELECT CASE WHEN status_updated_at > $2 THEN 1 ELSE 0 END
		   FROM runs
		  WHERE id = $1`,
		runID,
		oldActivity,
	).Scan(&touched); err != nil {
		t.Fatalf("query activity: %v", err)
	}
	if touched != 1 {
		t.Fatal("expected status_updated_at to be refreshed")
	}
}

func seedDesktopAccount(t *testing.T, pool *sqlitepgx.Pool, accountID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
		accountID,
		"desktop-test-"+accountID.String(),
		"Desktop Test",
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
}

func seedDesktopProject(t *testing.T, pool *sqlitepgx.Pool, accountID, projectID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
		projectID,
		accountID,
		"Desktop Project",
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
}

func seedDesktopThread(t *testing.T, pool *sqlitepgx.Pool, accountID, projectID, threadID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
		threadID,
		accountID,
		projectID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
}

func seedDesktopRun(t *testing.T, pool *sqlitepgx.Pool, accountID, threadID, runID uuid.UUID, parentRunID *uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, parent_run_id, status) VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		accountID,
		threadID,
		parentRunID,
	); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}
