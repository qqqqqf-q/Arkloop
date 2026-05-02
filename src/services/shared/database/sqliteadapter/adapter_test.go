//go:build desktop

package sqliteadapter

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"arkloop/services/shared/database"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *Pool {
	t.Helper()
	pool, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func migratedTestDB(t *testing.T) *Pool {
	t.Helper()
	pool, err := AutoMigrate(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("auto-migrate test db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func createTestTable(t *testing.T, pool *Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`CREATE TABLE test_items (id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now')))`)
	if err != nil {
		t.Fatalf("create test table: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Pool / Open
// ---------------------------------------------------------------------------

func TestOpen(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)

	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestOpen_Pragmas(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()

	// In-memory databases cannot use WAL; they report "memory".
	var journalMode string
	if err := pool.QueryRow(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "memory" {
		t.Errorf("journal_mode = %q; want %q", journalMode, "memory")
	}

	var fk int
	if err := pool.QueryRow(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d; want 1", fk)
	}
}

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

func TestAutoMigrate(t *testing.T) {
	t.Parallel()
	pool := migratedTestDB(t)
	ctx := context.Background()

	// Verify that at least one application table exists after the orgs -> accounts rename.
	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='accounts'`).Scan(&count)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if count != 1 {
		t.Fatalf("accounts table not found after auto-migrate")
	}

	// Verify _sequences table exists (needed by SQLiteDialect.Sequence()).
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM _sequences WHERE name = 'run_events_seq_global'`).Scan(&count)
	if err != nil {
		t.Fatalf("query _sequences: %v", err)
	}
	if count != 1 {
		t.Fatalf("run_events_seq_global row not found in _sequences after auto-migrate")
	}

	for _, tableName := range []string{
		"channels",
		"channel_identities",
		"channel_identity_bind_codes",
		"channel_dm_threads",
		"channel_message_receipts",
		"channel_message_ledger",
	} {
		err = pool.QueryRow(ctx,
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`,
			tableName,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query sqlite_master for %s: %v", tableName, err)
		}
		if count != 1 {
			t.Fatalf("%s table not found after auto-migrate", tableName)
		}
	}

	columns, err := sqliteTableColumns(ctx, pool.Unwrap(), "platform_settings")
	if err != nil {
		t.Fatalf("load platform_settings columns: %v", err)
	}
	if !hasSQLiteColumns(columns, "key", "value", "updated_at") {
		t.Fatalf("platform_settings columns = %v, want key/value/updated_at", columns)
	}

	assertSQLiteForeignKeyTargets(t, ctx, pool, "run_events", "runs")
	assertSQLiteForeignKeyTargets(t, ctx, pool, "sub_agent_events", "sub_agents", "runs")
	assertSQLiteForeignKeyTargets(t, ctx, pool, "sub_agent_pending_inputs", "sub_agents")
	assertSQLiteForeignKeyTargets(t, ctx, pool, "sub_agent_context_snapshots", "sub_agents")
	assertSQLiteForeignKeyTargets(t, ctx, pool, "channel_message_ledger", "channels", "threads", "runs", "channel_identities", "messages")
	assertSQLiteTableSQLContains(t, ctx, pool, "messages", "PRIMARY KEY DEFAULT (lower(hex(randomblob(4)))")

	// PRAGMA foreign_key_check must return zero violations after all migrations.
	rows, fkErr := pool.Query(ctx, "PRAGMA foreign_key_check")
	if fkErr != nil {
		t.Fatalf("foreign_key_check: %v", fkErr)
	}
	defer rows.Close()
	var fkViolations []string
	for rows.Next() {
		var table, rowid, parent string
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			t.Fatalf("foreign_key_check scan: %v", err)
		}
		fkViolations = append(fkViolations, fmt.Sprintf("%s(rowid=%s)->%s(fk=%d)", table, rowid, parent, fkid))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check rows: %v", err)
	}
	if len(fkViolations) > 0 {
		t.Fatalf("foreign key violations after migration: %v", fkViolations)
	}
}

func TestRepairMissingColumnsMigratesOldPlanMode(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `CREATE TABLE scheduled_triggers (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create scheduled_triggers: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE threads (id TEXT PRIMARY KEY, plan_mode BOOLEAN NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("create old threads: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, plan_mode) VALUES ('default-thread', 0), ('plan-thread', 1)`); err != nil {
		t.Fatalf("seed old threads: %v", err)
	}

	if err := repairMissingColumns(ctx, pool.Unwrap()); err != nil {
		t.Fatalf("repair missing columns: %v", err)
	}

	columns, err := sqliteTableColumns(ctx, pool.Unwrap(), "threads")
	if err != nil {
		t.Fatalf("load threads columns: %v", err)
	}
	if !hasSQLiteColumns(columns, "collaboration_mode", "collaboration_mode_revision", "learning_mode_enabled") {
		t.Fatalf("threads columns = %v, want repaired thread columns", columns)
	}

	var defaultMode, planMode string
	if err := pool.QueryRow(ctx, `SELECT collaboration_mode FROM threads WHERE id = 'default-thread'`).Scan(&defaultMode); err != nil {
		t.Fatalf("query default mode: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT collaboration_mode FROM threads WHERE id = 'plan-thread'`).Scan(&planMode); err != nil {
		t.Fatalf("query plan mode: %v", err)
	}
	if defaultMode != "default" || planMode != "plan" {
		t.Fatalf("collaboration modes = default:%q plan:%q, want default/plan", defaultMode, planMode)
	}
}

func TestMigrations_UpDown(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()
	db := pool.Unwrap()

	// Up
	results, err := Up(ctx, db)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("up returned zero results")
	}

	ver, err := CurrentVersion(ctx, db)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if ver != ExpectedVersion {
		t.Errorf("version after up = %d; want %d", ver, ExpectedVersion)
	}

	// DownAll
	count, err := DownAll(ctx, db)
	if err != nil {
		t.Fatalf("down all: %v", err)
	}
	if count == 0 {
		t.Fatal("down all rolled back zero migrations")
	}

	ver, err = CurrentVersion(ctx, db)
	if err != nil {
		t.Fatalf("current version after down: %v", err)
	}
	if ver != 0 {
		t.Errorf("version after down all = %d; want 0", ver)
	}
}

func TestMigration00089WebSearchBasicProvider(t *testing.T) {
	t.Parallel()

	pool := openTestDB(t)
	ctx := context.Background()
	db := pool.Unwrap()

	provider, err := newProvider(db)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 88); err != nil {
		t.Fatalf("up to 88: %v", err)
	}

	accountID := "00000000-0000-4000-8000-000000000089"
	ownerID := "00000000-0000-4000-8000-000000000090"
	conflictOwnerID := "00000000-0000-4000-8000-000000000091"
	for _, userID := range []string{ownerID, conflictOwnerID} {
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, username) VALUES (?, ?)`, userID, "user-"+userID[len(userID)-4:]); err != nil {
			t.Fatalf("insert user %s: %v", userID, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, slug, name, type, owner_user_id)
		VALUES (?, 'web-search-basic-migration', 'Web Search Basic Migration', 'personal', ?)
	`, accountID, ownerID); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	seedToolProvider := func(ownerKind string, ownerUserID *string, providerName string, active int, keyPrefix string, baseURL string, configJSON string) {
		t.Helper()
		var ownerArg any
		if ownerUserID != nil {
			ownerArg = *ownerUserID
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO tool_provider_configs (
				account_id, owner_kind, owner_user_id, group_name, provider_name,
				is_active, key_prefix, base_url, config_json
			) VALUES (?, ?, ?, 'web_search', ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)
		`, accountID, ownerKind, ownerArg, providerName, active, keyPrefix, baseURL, configJSON)
		if err != nil {
			t.Fatalf("insert provider %s/%s: %v", ownerKind, providerName, err)
		}
	}
	seedToolProvider("platform", nil, "web_search.duckduckgo", 1, "old-platform", "https://old.example", `{"mode":"old"}`)
	seedToolProvider("platform", nil, "web_search.basic", 0, "", "", `{}`)
	seedToolProvider("user", &ownerID, "web_search.duckduckgo", 0, "", "", `{"only":"old"}`)
	seedToolProvider("user", &conflictOwnerID, "web_search.duckduckgo", 1, "old-user", "", `{"mode":"old"}`)
	seedToolProvider("user", &conflictOwnerID, "web_search.basic", 0, "", "https://target.example", `{"keep":true}`)

	result, err := provider.UpByOne(ctx)
	if err != nil {
		t.Fatalf("apply 89: %v", err)
	}
	if result == nil || result.Source == nil || result.Source.Version != 89 {
		t.Fatalf("expected migration 89, got %#v", result)
	}

	assertSQLiteToolProviderCount(t, pool, ctx, "web_search.duckduckgo", 0)
	assertSQLiteToolProviderCount(t, pool, ctx, "web_search.basic", 3)

	var platformActive int
	var platformKey, platformBase, platformConfig string
	if err := pool.QueryRow(ctx, `
		SELECT is_active, COALESCE(key_prefix, ''), COALESCE(base_url, ''), config_json
		FROM tool_provider_configs
		WHERE owner_kind = 'platform' AND provider_name = 'web_search.basic'
	`).Scan(&platformActive, &platformKey, &platformBase, &platformConfig); err != nil {
		t.Fatalf("select platform row: %v", err)
	}
	if platformActive != 1 || platformKey != "old-platform" || platformBase != "https://old.example" || !strings.Contains(platformConfig, `"mode":"old"`) {
		t.Fatalf("platform row = active:%d key:%q base:%q config:%q", platformActive, platformKey, platformBase, platformConfig)
	}

	var userOnlyProvider string
	if err := pool.QueryRow(ctx, `
		SELECT provider_name
		FROM tool_provider_configs
		WHERE owner_kind = 'user' AND owner_user_id = ?
	`, ownerID).Scan(&userOnlyProvider); err != nil {
		t.Fatalf("select user-only row: %v", err)
	}
	if userOnlyProvider != "web_search.basic" {
		t.Fatalf("user-only provider = %q", userOnlyProvider)
	}

	var conflictActive int
	var conflictKey, conflictBase, conflictConfig string
	if err := pool.QueryRow(ctx, `
		SELECT is_active, COALESCE(key_prefix, ''), COALESCE(base_url, ''), config_json
		FROM tool_provider_configs
		WHERE owner_kind = 'user' AND owner_user_id = ? AND provider_name = 'web_search.basic'
	`, conflictOwnerID).Scan(&conflictActive, &conflictKey, &conflictBase, &conflictConfig); err != nil {
		t.Fatalf("select conflict row: %v", err)
	}
	if conflictActive != 1 || conflictKey != "old-user" || conflictBase != "https://target.example" ||
		!strings.Contains(conflictConfig, `"keep":true`) || strings.Contains(conflictConfig, `"mode":"old"`) {
		t.Fatalf("conflict row = active:%d key:%q base:%q config:%q", conflictActive, conflictKey, conflictBase, conflictConfig)
	}

	if _, err := provider.Down(ctx); err != nil {
		t.Fatalf("down 89: %v", err)
	}
	assertSQLiteToolProviderCount(t, pool, ctx, "web_search.basic", 0)
	assertSQLiteToolProviderCount(t, pool, ctx, "web_search.duckduckgo", 3)
}

func assertSQLiteToolProviderCount(t *testing.T, pool *Pool, ctx context.Context, providerName string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM tool_provider_configs WHERE provider_name = ?`, providerName).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", providerName, err)
	}
	if got != want {
		t.Fatalf("provider %s count = %d, want %d", providerName, got, want)
	}
}

func TestMigration00069RebuildsChannelMessageLedgerMessageForeignKey(t *testing.T) {
	t.Parallel()

	pool := openTestDB(t)
	ctx := context.Background()
	db := pool.Unwrap()

	provider, err := newProvider(db)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 68); err != nil {
		t.Fatalf("up to 68: %v", err)
	}

	_, _, _, _ = seedChannelLedgerFixture(t, ctx, pool, "00069")

	if err := checkForeignKeys(ctx, db); err != nil {
		t.Fatalf("foreign keys before 69: %v", err)
	}
	if _, err := provider.UpByOne(ctx); err != nil {
		t.Fatalf("apply 69: %v", err)
	}
	if err := checkForeignKeys(ctx, db); err != nil {
		t.Fatalf("foreign keys after 69: %v", err)
	}
	assertSQLiteForeignKeyTargets(t, ctx, pool, "channel_message_ledger", "channels", "threads", "runs", "channel_identities", "messages")
}

func TestMigration00070RepairsBrokenChannelMessageLedgerMessageForeignKey(t *testing.T) {
	t.Parallel()

	pool := openTestDB(t)
	ctx := context.Background()
	db := pool.Unwrap()

	provider, err := newProvider(db)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 69); err != nil {
		t.Fatalf("up to 69: %v", err)
	}

	accountID, threadID, channelID, messageID := seedChannelLedgerFixture(t, ctx, pool, "00070")

	if _, err := pool.Exec(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE channel_message_ledger RENAME TO channel_message_ledger_broken_00070`); err != nil {
		t.Fatalf("rename ledger: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE channel_message_ledger (
		id                         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
		channel_id                 TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		channel_type               TEXT NOT NULL,
		direction                  TEXT NOT NULL,
		thread_id                  TEXT REFERENCES threads(id) ON DELETE SET NULL,
		run_id                     TEXT REFERENCES runs(id) ON DELETE SET NULL,
		platform_conversation_id   TEXT NOT NULL,
		platform_message_id        TEXT NOT NULL,
		platform_parent_message_id TEXT,
		platform_thread_id         TEXT,
		sender_channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE SET NULL,
		metadata_json              TEXT NOT NULL DEFAULT '{}',
		created_at                 TEXT NOT NULL DEFAULT (datetime('now')),
		message_id                 TEXT REFERENCES messages_old_00069(id) ON DELETE SET NULL,
		CHECK (direction IN ('inbound', 'outbound')),
		UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
	)`); err != nil {
		t.Fatalf("create broken ledger: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_message_ledger (
		id, channel_id, channel_type, direction, thread_id, run_id, platform_conversation_id,
		platform_message_id, platform_parent_message_id, platform_thread_id,
		sender_channel_identity_id, metadata_json, created_at, message_id
	) SELECT
		id, channel_id, channel_type, direction, thread_id, run_id, platform_conversation_id,
		platform_message_id, platform_parent_message_id, platform_thread_id,
		sender_channel_identity_id, metadata_json, created_at, message_id
	FROM channel_message_ledger_broken_00070`); err != nil {
		t.Fatalf("copy broken ledger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE channel_message_ledger_broken_00070`); err != nil {
		t.Fatalf("drop broken ledger source: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX idx_channel_message_ledger_channel_id ON channel_message_ledger(channel_id)`); err != nil {
		t.Fatalf("create channel index: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX idx_channel_message_ledger_thread_id ON channel_message_ledger(thread_id)`); err != nil {
		t.Fatalf("create thread index: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX idx_channel_message_ledger_run_id ON channel_message_ledger(run_id)`); err != nil {
		t.Fatalf("create run index: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX idx_channel_message_ledger_sender_identity_id ON channel_message_ledger(sender_channel_identity_id)`); err != nil {
		t.Fatalf("create sender index: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX idx_channel_message_ledger_message_id ON channel_message_ledger(message_id)`); err != nil {
		t.Fatalf("create message index: %v", err)
	}
	if _, err := pool.Exec(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	if err := checkForeignKeys(ctx, db); err == nil {
		t.Fatal("expected broken ledger foreign key violations before 70 repair")
	}
	if _, err := provider.UpByOne(ctx); err != nil {
		t.Fatalf("apply 70: %v", err)
	}
	if err := checkForeignKeys(ctx, db); err != nil {
		t.Fatalf("foreign keys after 70: %v", err)
	}

	var linkedMessageID string
	if err := pool.QueryRow(ctx,
		`SELECT message_id FROM channel_message_ledger WHERE channel_id = ? AND thread_id = ?`,
		channelID,
		threadID,
	).Scan(&linkedMessageID); err != nil {
		t.Fatalf("load repaired ledger row: %v", err)
	}
	if linkedMessageID != messageID {
		t.Fatalf("ledger message_id = %q; want %q", linkedMessageID, messageID)
	}
	assertSQLiteForeignKeyTargets(t, ctx, pool, "channel_message_ledger", "channels", "threads", "runs", "channel_identities", "messages")
	_ = accountID
}

func TestMigration00071RepairsBrokenMessagesIDDefault(t *testing.T) {
	t.Parallel()

	pool := openTestDB(t)
	ctx := context.Background()
	db := pool.Unwrap()

	provider, err := newProvider(db)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 70); err != nil {
		t.Fatalf("up to 70: %v", err)
	}

	accountID, threadID, channelID, messageID := seedChannelLedgerFixture(t, ctx, pool, "00071")

	if _, err := pool.Exec(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE messages RENAME TO messages_broken_00071`); err != nil {
		t.Fatalf("rename messages: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE messages (
		id                 TEXT PRIMARY KEY,
		thread_id          TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
		account_id         TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		thread_seq         INTEGER NOT NULL,
		created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		role               TEXT NOT NULL,
		content            TEXT NOT NULL,
		content_json       TEXT,
		metadata_json      TEXT NOT NULL DEFAULT '{}',
		hidden             INTEGER NOT NULL DEFAULT 0,
		deleted_at         TEXT,
		token_count        INTEGER,
		created_at         TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create broken messages: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (
		id, thread_id, account_id, thread_seq, created_by_user_id, role, content, content_json,
		metadata_json, hidden, deleted_at, token_count, created_at
	) SELECT
		id, thread_id, account_id, thread_seq, created_by_user_id, role, content, content_json,
		metadata_json, hidden, deleted_at, token_count, created_at
	FROM messages_broken_00071`); err != nil {
		t.Fatalf("copy broken messages: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE messages_broken_00071`); err != nil {
		t.Fatalf("drop broken messages source: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX ix_messages_thread_id ON messages(thread_id)`); err != nil {
		t.Fatalf("create ix_messages_thread_id: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX ix_messages_org_id_thread_id_created_at ON messages(account_id, thread_id, created_at)`); err != nil {
		t.Fatalf("create ix_messages_org_id_thread_id_created_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX ix_messages_account_id_thread_id_thread_seq ON messages(account_id, thread_id, thread_seq)`); err != nil {
		t.Fatalf("create ix_messages_account_id_thread_id_thread_seq: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX ix_messages_thread_id_thread_seq ON messages(thread_id, thread_seq)`); err != nil {
		t.Fatalf("create ix_messages_thread_id_thread_seq: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE UNIQUE INDEX uq_messages_thread_id_thread_seq ON messages(thread_id, thread_seq)`); err != nil {
		t.Fatalf("create uq_messages_thread_id_thread_seq: %v", err)
	}
	if _, err := pool.Exec(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	assertSQLiteTableSQLNotContains(t, ctx, pool, "messages", "PRIMARY KEY DEFAULT (lower(hex(randomblob(4)))")
	if err := checkForeignKeys(ctx, db); err == nil {
		t.Fatal("expected broken messages default rebuild to invalidate channel_message_ledger foreign keys before 71 repair")
	}
	if _, err := provider.UpByOne(ctx); err != nil {
		t.Fatalf("apply 71: %v", err)
	}
	if err := checkForeignKeys(ctx, db); err != nil {
		t.Fatalf("foreign keys after 71: %v", err)
	}

	assertSQLiteForeignKeyTargets(t, ctx, pool, "channel_message_ledger", "channels", "threads", "runs", "channel_identities", "messages")

	var messagesSQL string
	if err := pool.QueryRow(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'messages'`).Scan(&messagesSQL); err != nil {
		t.Fatalf("load messages schema: %v", err)
	}
	if !strings.Contains(messagesSQL, "DEFAULT (lower(hex(randomblob(4)))") {
		t.Fatalf("messages schema did not restore id default: %s", messagesSQL)
	}
	assertSQLiteTableSQLContains(t, ctx, pool, "messages", "PRIMARY KEY DEFAULT (lower(hex(randomblob(4)))")

	var linkedMessageID string
	if err := pool.QueryRow(ctx,
		`SELECT message_id FROM channel_message_ledger WHERE channel_id = ? AND thread_id = ?`,
		channelID,
		threadID,
	).Scan(&linkedMessageID); err != nil {
		t.Fatalf("load repaired ledger row: %v", err)
	}
	if linkedMessageID != messageID {
		t.Fatalf("ledger message_id = %q; want %q", linkedMessageID, messageID)
	}
	_ = accountID
}

func seedChannelLedgerFixture(t *testing.T, ctx context.Context, pool *Pool, suffix string) (string, string, string, string) {
	t.Helper()

	accountID := fmt.Sprintf("00000000-0000-4000-8000-0000000%05s1", suffix)
	threadID := fmt.Sprintf("00000000-0000-4000-8000-0000000%05s2", suffix)
	channelID := fmt.Sprintf("00000000-0000-4000-8000-0000000%05s3", suffix)
	messageID := fmt.Sprintf("00000000-0000-4000-8000-0000000%05s4", suffix)
	ledgerID := fmt.Sprintf("00000000-0000-4000-8000-0000000%05s5", suffix)

	if _, err := pool.Exec(ctx,
		`INSERT INTO accounts (id, slug, name, type, status) VALUES (?, ?, ?, 'personal', 'active')`,
		accountID, "acct-"+suffix, "acct-"+suffix,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO threads (id, account_id, title, mode, next_message_seq) VALUES (?, ?, ?, 'chat', 2)`,
		threadID, accountID, "thread-"+suffix,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO channels (id, account_id, channel_type, is_active, config_json) VALUES (?, ?, ?, 1, '{}')`,
		channelID, accountID, "telegram-"+suffix,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO messages (id, thread_id, account_id, thread_seq, role, content, metadata_json, hidden) VALUES (?, ?, ?, 1, 'user', ?, '{}', 0)`,
		messageID, threadID, accountID, "hello-"+suffix,
	); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_message_ledger (
			id, channel_id, channel_type, direction, thread_id, platform_conversation_id,
			platform_message_id, metadata_json, message_id
		) VALUES (?, ?, ?, 'outbound', ?, ?, ?, '{}', ?)`,
		ledgerID,
		channelID,
		"telegram-"+suffix,
		threadID,
		"chat-"+suffix,
		"msg-"+suffix,
		messageID,
	); err != nil {
		t.Fatalf("insert channel ledger row: %v", err)
	}
	return accountID, threadID, channelID, messageID
}

// ---------------------------------------------------------------------------
// Exec / Query / QueryRow
// ---------------------------------------------------------------------------

func TestExec(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()
	createTestTable(t, pool)

	res, err := pool.Exec(ctx, `INSERT INTO test_items (id, name) VALUES ('1', 'alpha')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if res.RowsAffected() != 1 {
		t.Errorf("rows affected = %d; want 1", res.RowsAffected())
	}
}

func TestQueryRow(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()
	createTestTable(t, pool)

	_, err := pool.Exec(ctx, `INSERT INTO test_items (id, name) VALUES ('1', 'alpha')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var id, name string
	if err := pool.QueryRow(ctx, `SELECT id, name FROM test_items WHERE id = '1'`).Scan(&id, &name); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if id != "1" || name != "alpha" {
		t.Errorf("got id=%q name=%q; want id=%q name=%q", id, name, "1", "alpha")
	}
}

func TestQueryRow_NoRows(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()
	createTestTable(t, pool)

	var id string
	err := pool.QueryRow(ctx, `SELECT id FROM test_items WHERE id = 'nope'`).Scan(&id)
	if err == nil {
		t.Fatal("expected error for missing row, got nil")
	}
	if !database.IsNoRows(err) {
		t.Errorf("expected database.ErrNoRows; got %v", err)
	}
}

func TestQuery(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()
	createTestTable(t, pool)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		_, err := pool.Exec(ctx, `INSERT INTO test_items (id, name) VALUES (?, ?)`, name, name)
		if err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}

	rows, err := pool.Query(ctx, `SELECT id, name FROM test_items ORDER BY name`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows; want 3", len(got))
	}
	if got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Errorf("got %v; want [alpha beta gamma]", got)
	}
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

func TestTransaction_Commit(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()
	createTestTable(t, pool)

	txn, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = txn.Exec(ctx, `INSERT INTO test_items (id, name) VALUES ('1', 'alpha')`)
	if err != nil {
		t.Fatalf("exec in tx: %v", err)
	}
	if err := txn.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM test_items WHERE id = '1'`).Scan(&name); err != nil {
		t.Fatalf("select after commit: %v", err)
	}
	if name != "alpha" {
		t.Errorf("name = %q; want %q", name, "alpha")
	}
}

func TestTransaction_Rollback(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()
	createTestTable(t, pool)

	txn, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = txn.Exec(ctx, `INSERT INTO test_items (id, name) VALUES ('1', 'alpha')`)
	if err != nil {
		t.Fatalf("exec in tx: %v", err)
	}
	if err := txn.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM test_items`).Scan(&count); err != nil {
		t.Fatalf("select after rollback: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d; want 0 after rollback", count)
	}
}

// ---------------------------------------------------------------------------
// Dialect
// ---------------------------------------------------------------------------

func TestDialect_Name(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	if d.Name() != database.DialectSQLite {
		t.Errorf("Name() = %q; want %q", d.Name(), database.DialectSQLite)
	}
}

func TestDialect_Placeholder(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	tests := []struct {
		index int
		want  string
	}{
		{1, "?1"},
		{3, "?3"},
		{10, "?10"},
	}
	for _, tt := range tests {
		if got := d.Placeholder(tt.index); got != tt.want {
			t.Errorf("Placeholder(%d) = %q; want %q", tt.index, got, tt.want)
		}
	}
}

func TestDialect_Now(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	if got := d.Now(); got != "datetime('now')" {
		t.Errorf("Now() = %q; want %q", got, "datetime('now')")
	}
}

func TestDialect_IntervalAdd(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	got := d.IntervalAdd("created_at", "24 hours", "+24 hours")
	want := "datetime(created_at, '+24 hours')"
	if got != want {
		t.Errorf("IntervalAdd() = %q; want %q", got, want)
	}
}

func TestDialect_JSONCast(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	expr := "some_column"
	if got := d.JSONCast(expr); got != expr {
		t.Errorf("JSONCast(%q) = %q; want %q (no-op)", expr, got, expr)
	}
}

func TestDialect_ForUpdate(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	if got := d.ForUpdate(); got != "" {
		t.Errorf("ForUpdate() = %q; want empty string", got)
	}
}

func TestDialect_ILike(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	if got := d.ILike(); got != "LIKE" {
		t.Errorf("ILike() = %q; want %q", got, "LIKE")
	}
}

func TestDialect_ArrayAny(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}
	got := d.ArrayAny("status", 2)
	want := "EXISTS(SELECT 1 FROM json_each(?2) WHERE value = status)"
	if got != want {
		t.Errorf("ArrayAny() = %q; want %q", got, want)
	}
}

func TestDialect_OnConflict(t *testing.T) {
	t.Parallel()
	d := SQLiteDialect{}

	gotUpdate := d.OnConflictDoUpdate("id", "name = excluded.name")
	wantUpdate := "ON CONFLICT (id) DO UPDATE SET name = excluded.name"
	if gotUpdate != wantUpdate {
		t.Errorf("OnConflictDoUpdate() = %q; want %q", gotUpdate, wantUpdate)
	}

	gotNothing := d.OnConflictDoNothing("id")
	wantNothing := "ON CONFLICT (id) DO NOTHING"
	if gotNothing != wantNothing {
		t.Errorf("OnConflictDoNothing() = %q; want %q", gotNothing, wantNothing)
	}
}

// ---------------------------------------------------------------------------
// Embedded migrations metadata
// ---------------------------------------------------------------------------

func TestEmbeddedMigrations(t *testing.T) {
	t.Parallel()
	if ExpectedVersion <= 0 {
		t.Errorf("ExpectedVersion = %d; want > 0", ExpectedVersion)
	}
	if EmbeddedMigrationCount <= 0 {
		t.Errorf("EmbeddedMigrationCount = %d; want > 0", EmbeddedMigrationCount)
	}
}

func assertSQLiteForeignKeyTargets(t *testing.T, ctx context.Context, pool *Pool, table string, want ...string) {
	t.Helper()

	rows, err := pool.Query(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%s)", table))
	if err != nil {
		t.Fatalf("foreign_key_list %s: %v", table, err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var (
			id       int
			seq      int
			target   string
			fromCol  string
			toCol    string
			onUpdate string
			onDelete string
			match    string
		)
		if err := rows.Scan(&id, &seq, &target, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign_key_list %s: %v", table, err)
		}
		got[target] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows foreign_key_list %s: %v", table, err)
	}

	if len(got) != len(want) {
		t.Fatalf("%s foreign key targets = %v; want %v", table, got, want)
	}
	for _, target := range want {
		if !got[target] {
			t.Fatalf("%s foreign key targets = %v; missing %s", table, got, target)
		}
	}
}

func sqliteTableSQL(t *testing.T, ctx context.Context, pool *Pool, table string) string {
	t.Helper()

	var sql string
	if err := pool.QueryRow(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&sql); err != nil {
		t.Fatalf("load sqlite_master sql for %s: %v", table, err)
	}
	return sql
}

func assertSQLiteTableSQLContains(t *testing.T, ctx context.Context, pool *Pool, table string, needle string) {
	t.Helper()
	assertSQLiteTableSQLMatch(t, ctx, pool, table, needle, true)
}

func assertSQLiteTableSQLNotContains(t *testing.T, ctx context.Context, pool *Pool, table string, needle string) {
	t.Helper()
	assertSQLiteTableSQLMatch(t, ctx, pool, table, needle, false)
}

func assertSQLiteTableSQLMatch(t *testing.T, ctx context.Context, pool *Pool, table string, needle string, want bool) {
	t.Helper()
	var sql string
	if err := pool.QueryRow(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&sql); err != nil {
		t.Fatalf("load sqlite_master sql for %s: %v", table, err)
	}
	has := strings.Contains(sql, needle)
	if has != want {
		t.Fatalf("table %s SQL contains %q = %v; sql=%s", table, needle, has, sql)
	}
}
