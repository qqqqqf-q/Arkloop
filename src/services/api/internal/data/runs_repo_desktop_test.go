//go:build desktop

package data

import (
	"context"
	"encoding/json"
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

func TestProvideInputWithKeyDedupesRepeatedInput(t *testing.T) {
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
			args: []any{accountID, "runs-input-key-" + accountID.String(), "Runs Input Key"},
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

	repo, err := NewRunEventRepository(db)
	if err != nil {
		t.Fatalf("new run event repo: %v", err)
	}

	first, err := repo.ProvideInputWithKey(ctx, runID, "hello", "trace-1", "telegram:chat-1:msg-1")
	if err != nil {
		t.Fatalf("first ProvideInputWithKey: %v", err)
	}
	if first == nil {
		t.Fatal("expected first input event")
	}

	second, err := repo.ProvideInputWithKey(ctx, runID, "hello", "trace-2", "telegram:chat-1:msg-1")
	if err != nil {
		t.Fatalf("second ProvideInputWithKey: %v", err)
	}
	if second != nil {
		t.Fatalf("expected duplicate input to return nil event, got %#v", second)
	}

	var count int
	if err := db.QueryRow(ctx,
		`SELECT COUNT(*) FROM run_events WHERE run_id = $1 AND type = 'run.input_provided'`,
		runID,
	).Scan(&count); err != nil {
		t.Fatalf("count run input events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 input event, got %d", count)
	}
}

func TestRequestCancelRecordsVisibleSeqCutoff(t *testing.T) {
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
			args: []any{accountID, "runs-cancel-" + accountID.String(), "Runs Cancel"},
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

	repo, err := NewRunEventRepository(db)
	if err != nil {
		t.Fatalf("new run event repo: %v", err)
	}

	clientCancelledAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := repo.RequestCancel(ctx, runID, nil, "trace-123", 7, &clientCancelledAt); err != nil {
		t.Fatalf("request cancel: %v", err)
	}

	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("load run status: %v", err)
	}
	if status != "cancelling" {
		t.Fatalf("expected status cancelling, got %q", status)
	}

	var dataJSON []byte
	if err := db.QueryRow(ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.cancel_requested' LIMIT 1`,
		runID,
	).Scan(&dataJSON); err != nil {
		t.Fatalf("load cancel event: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(dataJSON, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got, ok := payload["visible_seq_cutoff"].(float64); !ok || int64(got) != 7 {
		t.Fatalf("unexpected visible_seq_cutoff: %#v", payload["visible_seq_cutoff"])
	}
	if got, ok := payload["last_seen_seq"].(float64); !ok || int64(got) != 7 {
		t.Fatalf("unexpected last_seen_seq: %#v", payload["last_seen_seq"])
	}
	if got, ok := payload["client_cancelled_at"].(string); !ok || got != clientCancelledAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected client_cancelled_at: %#v", payload["client_cancelled_at"])
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
