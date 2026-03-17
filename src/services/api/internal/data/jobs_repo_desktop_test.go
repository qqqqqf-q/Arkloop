//go:build desktop

package data

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/desktop"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type stubDesktopEnqueuer struct {
	callCount int
	accountID uuid.UUID
	runID     uuid.UUID
	traceID   string
	jobType   string
	payload   map[string]any
}

func (s *stubDesktopEnqueuer) EnqueueRun(
	_ context.Context,
	accountID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	_ *time.Time,
) (uuid.UUID, error) {
	s.callCount++
	s.accountID = accountID
	s.runID = runID
	s.traceID = traceID
	s.jobType = queueJobType
	s.payload = payload
	return uuid.New(), nil
}

func TestJobRepositoryDesktopRunExecuteBypassesPersistentJobs(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	prev := desktop.GetJobEnqueuer()
	stub := &stubDesktopEnqueuer{}
	desktop.SetJobEnqueuer(stub)
	defer desktop.SetJobEnqueuer(prev)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	repo, err := NewJobRepository(tx)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}

	accountID := uuid.New()
	runID := uuid.New()
	if _, err := repo.EnqueueRun(ctx, accountID, runID, "", RunExecuteJobType, map[string]any{"source": "test"}, nil); err != nil {
		t.Fatalf("enqueue run: %v", err)
	}

	var beforeCommit int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&beforeCommit); err != nil {
		t.Fatalf("count jobs before commit: %v", err)
	}
	if beforeCommit != 0 {
		t.Fatalf("expected no persisted jobs before commit, got %d", beforeCommit)
	}
	if stub.callCount != 0 {
		t.Fatalf("expected after-commit enqueue, got %d calls before commit", stub.callCount)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	var afterCommit int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&afterCommit); err != nil {
		t.Fatalf("count jobs after commit: %v", err)
	}
	if afterCommit != 0 {
		t.Fatalf("expected desktop run.execute to skip jobs table, got %d rows", afterCommit)
	}
	if stub.callCount != 1 {
		t.Fatalf("expected one enqueue call after commit, got %d", stub.callCount)
	}
	if stub.accountID != accountID || stub.runID != runID {
		t.Fatalf("unexpected enqueue target: account=%s run=%s", stub.accountID, stub.runID)
	}
	if stub.jobType != RunExecuteJobType {
		t.Fatalf("unexpected job type: %s", stub.jobType)
	}
	if stub.traceID == "" {
		t.Fatal("expected normalized trace id to be generated")
	}
	if got, _ := stub.payload["source"].(string); got != "test" {
		t.Fatalf("unexpected payload: %#v", stub.payload)
	}
}
