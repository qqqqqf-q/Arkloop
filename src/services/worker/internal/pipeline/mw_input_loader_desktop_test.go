//go:build desktop

package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

func TestLoadRunInputsDesktopBoundsFreshChannelHistoryAtThreadTail(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "bounded-input.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	if _, err := db.Exec(ctx, `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`, accountID, "acc-"+accountID.String(), "acc"); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		fmt.Sprintf(`{"thread_tail_message_id":"%s","channel_delivery":{"channel_id":"%s"}}`, msg2ID, uuid.New()),
	); err != nil {
		t.Fatalf("insert run started: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, created_at) VALUES ($1, $2, $3, 'user', 'one', '{}', FALSE, $4)`, msg1ID, accountID, threadID, "2026-04-09 05:18:30.100000000 +0000"); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, created_at) VALUES ($1, $2, $3, 'user', 'two', '{}', FALSE, $4)`, msg2ID, accountID, threadID, "2026-04-09 05:18:31.100000000 +0000"); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, created_at) VALUES ($1, $2, $3, 'assistant', 'future assistant', '{}', FALSE, $4)`, msg3ID, accountID, threadID, "2026-04-09 05:18:32.100000000 +0000"); err != nil {
		t.Fatalf("insert future assistant: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO thread_compaction_snapshots (account_id, thread_id, summary_text, metadata_json, is_active) VALUES ($1, $2, 'future summary', '{}', 1)`, accountID, threadID); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	loaded, err := loadRunInputs(ctx, db, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.DesktopRunsRepository{}, data.DesktopRunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 bounded messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "one" {
		t.Fatalf("unexpected first bounded message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "two" {
		t.Fatalf("unexpected second bounded message: %#v", loaded.Messages[1])
	}
	if len(loaded.ThreadMessageIDs) != 2 || loaded.ThreadMessageIDs[0] != msg1ID || loaded.ThreadMessageIDs[1] != msg2ID {
		t.Fatalf("unexpected bounded thread ids: %#v", loaded.ThreadMessageIDs)
	}
	if loaded.HasActiveCompactSnapshot {
		t.Fatal("expected bounded channel run to skip active compact snapshot")
	}
}
