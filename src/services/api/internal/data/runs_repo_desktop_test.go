//go:build desktop

package data

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

func TestProvideInputRejectsCanceledRun(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openRunsRepoTestDB(t, ctx)
	defer cleanup()

	runID := uuid.New()
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID, "runs-provide-input-" + accountID.String(), "Runs Provide Input"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID, accountID, "Runs Project"},
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
			t.Fatalf("seed run data: %v", err)
		}
	}

	eventID := uuid.New()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(ctx,
		`INSERT INTO run_events (event_id, run_id, seq, ts, type, data_json) VALUES ($1, $2, 1, $3, 'run.cancel_requested', '{}')`,
		eventID, runID, now,
	); err != nil {
		t.Fatalf("insert cancel event: %v", err)
	}

	repo, err := NewRunEventRepository(db)
	if err != nil {
		t.Fatalf("new run event repo: %v", err)
	}

	if _, err := repo.ProvideInput(ctx, runID, "input after cancel", "trace"); err == nil {
		t.Fatalf("expected ProvideInput to reject canceling run")
	} else {
		var notActive RunNotActiveError
		if !errors.As(err, &notActive) {
			t.Fatalf("expected RunNotActiveError, got %T", err)
		}
	}
}

func openRunsRepoTestDB(t *testing.T, ctx context.Context) (*sqlitepgx.Pool, func()) {
	t.Helper()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	cleanup := func() {
		_ = sqlitePool.Close()
	}
	return sqlitepgx.New(sqlitePool.Unwrap()), cleanup
}
