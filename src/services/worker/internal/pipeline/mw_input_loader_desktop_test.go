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

func TestLoadRunInputsDesktopResolvesChannelHistoryUpperBoundFromLedger(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "bounded-ledger.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`, []any{accountID, "acc-" + accountID.String(), "acc"}},
		{`INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
		{`INSERT INTO channels (id, account_id, channel_type, is_active, config_json) VALUES ($1, $2, 'telegram', 1, '{}')`, []any{channelID, accountID}},
		{`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata) VALUES ($1, 'telegram', $2, '{}')`, []any{identityID, "chat-1-user"}},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	runStarted := fmt.Sprintf(`{
		"channel_delivery":{
			"channel_id":"%s",
			"channel_type":"telegram",
			"sender_channel_identity_id":"%s",
			"conversation_type":"supergroup",
			"conversation_ref":{"target":"chat-1"},
			"trigger_message_ref":{"message_id":"m-2"},
			"inbound_message_ref":{"message_id":"m-2"}
		}
	}`, channelID, identityID)
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, runStarted); err != nil {
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
	if _, err := db.Exec(ctx, `INSERT INTO channel_message_ledger (channel_id, channel_type, direction, thread_id, platform_conversation_id, platform_message_id, sender_channel_identity_id, message_id, metadata_json, created_at) VALUES ($1, 'telegram', 'inbound', $2, 'chat-1', 'm-2', $3, $4, '{}', $5)`, channelID, threadID, identityID, msg2ID, "2026-04-09 05:18:31.200000000 +0000"); err != nil {
		t.Fatalf("insert ledger row: %v", err)
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
	if got := loaded.InputJSON[runStartedThreadTailMessageIDKey]; got != msg2ID.String() {
		t.Fatalf("unexpected resolved thread tail: %#v", got)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 bounded messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content[0].Text != "one" || loaded.Messages[1].Content[0].Text != "two" {
		t.Fatalf("unexpected bounded contents: %#v", loaded.Messages)
	}
	if loaded.HasActiveCompactSnapshot {
		t.Fatal("expected resolved bounded channel run to skip active compact snapshot")
	}
}

func TestLoadRunInputsDesktopSkipsSnapshotWhenChannelUpperBoundMissing(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "channel-no-upper-bound.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`, []any{accountID, "acc-" + accountID.String(), "acc"}},
		{`INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
		{`INSERT INTO channels (id, account_id, channel_type, is_active, config_json) VALUES ($1, $2, 'telegram', 1, '{}')`, []any{channelID, accountID}},
		{`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata) VALUES ($1, 'telegram', $2, '{}')`, []any{identityID, "chat-1-user"}},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	runStarted := fmt.Sprintf(`{
		"channel_delivery":{
			"channel_id":"%s",
			"channel_type":"telegram",
			"sender_channel_identity_id":"%s",
			"conversation_type":"supergroup",
			"conversation_ref":{"target":"chat-1"},
			"trigger_message_ref":{"message_id":"missing"},
			"inbound_message_ref":{"message_id":"missing"}
		}
	}`, channelID, identityID)
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, runStarted); err != nil {
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
	if loaded.HasActiveCompactSnapshot {
		t.Fatal("expected channel downgrade path to skip active compact snapshot")
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected full visible history without snapshot, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content[0].Text != "one" || loaded.Messages[2].Content[0].Text != "future assistant" {
		t.Fatalf("unexpected downgrade contents: %#v", loaded.Messages)
	}
}
