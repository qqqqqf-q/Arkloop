//go:build desktop

package data

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestDesktopRunEventsRepositoryAppendEventWritesHighPrecisionTimestamp(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, t.TempDir()+"/run-events.db")
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()
	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedDesktopAccount(t, db, accountID)
	seedDesktopProject(t, db, accountID, projectID)
	seedDesktopThread(t, db, accountID, projectID, threadID)
	seedDesktopRun(t, db, accountID, threadID, runID, nil)

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := (DesktopRunEventsRepository{}).AppendEvent(ctx, tx, runID, "worker.job.received", nil, nil, nil); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	var rawTS string
	if err := db.QueryRow(ctx, `SELECT ts FROM run_events WHERE run_id = $1 AND type = 'worker.job.received'`, runID).Scan(&rawTS); err != nil {
		t.Fatalf("query event ts: %v", err)
	}
	if !strings.Contains(rawTS, ".") {
		t.Fatalf("expected high precision timestamp, got %q", rawTS)
	}
}
