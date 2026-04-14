//go:build desktop

package subagentctl

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestCreateChildThreadDesktop(t *testing.T) {
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

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID, "desktop-subagent-" + accountID.String(), "Desktop SubAgent"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID, accountID, "SubAgent Project"},
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
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed desktop data: %v", err)
		}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	factory := &SubAgentRunFactory{}
	parentRun := data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
		ProjectID: &projectID,
	}

	childThreadID, err := factory.createChildThread(ctx, tx, parentRun)
	if err != nil {
		t.Fatalf("create child thread: %v", err)
	}

	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT expires_at FROM threads WHERE id = $1`,
		childThreadID,
	).Scan(&expiresAt)
	if err != nil {
		t.Fatalf("load child thread expires_at: %v", err)
	}
	if expiresAt.Before(time.Now().UTC().Add(6 * 24 * time.Hour)) {
		t.Fatalf("unexpected expires_at: %s", expiresAt.Format(time.RFC3339))
	}
}
