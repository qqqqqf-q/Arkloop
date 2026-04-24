package migrate

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"arkloop/services/api/internal/testutil"
	"github.com/google/uuid"
)

func TestUpFromScratch(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_up")
	ctx := context.Background()

	results, err := Up(ctx, db.DSN)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(results) != EmbeddedMigrationCount {
		t.Fatalf("expected %d applied migrations, got %d", EmbeddedMigrationCount, len(results))
	}

	version, err := CurrentVersion(ctx, db.DSN)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if version != ExpectedVersion {
		t.Fatalf("expected version %d, got %d", ExpectedVersion, version)
	}
}

func TestUpIdempotent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_idem")
	ctx := context.Background()

	if _, err := Up(ctx, db.DSN); err != nil {
		t.Fatalf("first up: %v", err)
	}

	results, err := Up(ctx, db.DSN)
	if err != nil {
		t.Fatalf("second up: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 migrations on second run, got %d", len(results))
	}
}

func TestDownOne(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_down")
	ctx := context.Background()

	if _, err := Up(ctx, db.DSN); err != nil {
		t.Fatalf("up: %v", err)
	}

	result, err := DownOne(ctx, db.DSN)
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if result == nil {
		t.Fatal("expected a migration result")
	}

	version, err := CurrentVersion(ctx, db.DSN)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	prevVersion := ExpectedVersion - 1
	if version != prevVersion {
		t.Fatalf("expected version %d, got %d", prevVersion, version)
	}
}

func TestCheckVersionMatch(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_check_match")
	ctx := context.Background()

	if _, err := Up(ctx, db.DSN); err != nil {
		t.Fatalf("up: %v", err)
	}

	current, expected, match, err := CheckVersion(ctx, db.DSN)
	if err != nil {
		t.Fatalf("check version: %v", err)
	}
	if !match {
		t.Fatalf("expected match: current=%d expected=%d", current, expected)
	}
}

func TestCheckVersionMismatch(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_check_mismatch")
	ctx := context.Background()

	if _, err := Up(ctx, db.DSN); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := DownOne(ctx, db.DSN); err != nil {
		t.Fatalf("down: %v", err)
	}

	_, _, match, err := CheckVersion(ctx, db.DSN)
	if err != nil {
		t.Fatalf("check version: %v", err)
	}
	if match {
		t.Fatal("expected mismatch after down")
	}
}

func TestFullRoundTrip(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_roundtrip")
	ctx := context.Background()

	// apply all
	upResults, err := Up(ctx, db.DSN)
	if err != nil {
		t.Fatalf("first up: %v", err)
	}
	if len(upResults) == 0 {
		t.Fatal("expected migrations on first up")
	}

	// rollback all
	downCount, err := DownAll(ctx, db.DSN)
	if err != nil {
		t.Fatalf("down all: %v", err)
	}
	if downCount != len(upResults) {
		t.Fatalf("down count %d != up count %d", downCount, len(upResults))
	}

	version, err := CurrentVersion(ctx, db.DSN)
	if err != nil {
		t.Fatalf("version after down all: %v", err)
	}
	if version != 0 {
		t.Fatalf("expected version 0 after down all, got %d", version)
	}

	// reapply all
	reapplyResults, err := Up(ctx, db.DSN)
	if err != nil {
		t.Fatalf("reapply up: %v", err)
	}
	if len(reapplyResults) != len(upResults) {
		t.Fatalf("reapply count %d != first up count %d", len(reapplyResults), len(upResults))
	}

	_, _, match, err := CheckVersion(ctx, db.DSN)
	if err != nil {
		t.Fatalf("check version after reapply: %v", err)
	}
	if !match {
		t.Fatal("version mismatch after reapply")
	}
}

func TestTablesExist(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_tables")
	ctx := context.Background()

	if _, err := Up(ctx, db.DSN); err != nil {
		t.Fatalf("up: %v", err)
	}

	conn, err := sql.Open("pgx", db.DSN)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = conn.Close() }()

	expectedTables := []string{
		"orgs",
		"users",
		"org_memberships",
		"threads",
		"messages",
		"runs",
		"run_events",
		"user_credentials",
		"audit_logs",
		"jobs",
		"secrets",
		"llm_credentials",
		"llm_routes",
		"mcp_configs",
		"personas",
		"worker_registrations",
		"ip_rules",
		"api_keys",
	}

	for _, table := range expectedTables {
		var exists bool
		err := conn.QueryRowContext(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`,
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s does not exist after migration", table)
		}
	}
}

func TestUpgradeChannelHeartbeatScopeMigration(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_channel_heartbeat_scope")
	ctx := context.Background()

	sqlDB, err := openDB(db.DSN)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	for _, stmt := range []string{
		`CREATE TABLE goose_db_version (
			id SERIAL PRIMARY KEY,
			version_id BIGINT NOT NULL,
			is_applied BOOLEAN NOT NULL,
			tstamp TIMESTAMP DEFAULT now()
		)`,
		`CREATE TABLE channel_identities (
			id UUID PRIMARY KEY,
			channel_type TEXT NOT NULL,
			platform_subject_id TEXT NOT NULL,
			heartbeat_enabled INTEGER NOT NULL DEFAULT 0,
			heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30,
			heartbeat_model TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE channels (
			id UUID PRIMARY KEY,
			account_id UUID NOT NULL
		)`,
		`CREATE TABLE threads (
			id UUID PRIMARY KEY,
			account_id UUID NOT NULL,
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE TABLE channel_dm_threads (
			channel_id UUID NOT NULL,
			channel_identity_id UUID NOT NULL,
			thread_id UUID NOT NULL
		)`,
		`CREATE TABLE channel_group_threads (
			channel_id UUID NOT NULL,
			platform_chat_id TEXT NOT NULL,
			persona_id UUID,
			thread_id UUID NOT NULL
		)`,
		`CREATE TABLE channel_identity_links (
			id UUID PRIMARY KEY,
			channel_id UUID NOT NULL,
			channel_identity_id UUID NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (channel_id, channel_identity_id)
		)`,
		`CREATE TABLE scheduled_triggers (
			id UUID PRIMARY KEY,
			channel_identity_id UUID NOT NULL,
			persona_key TEXT NOT NULL,
			account_id UUID NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			interval_min INT NOT NULL DEFAULT 30,
			next_fire_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX scheduled_triggers_channel_identity_id_idx ON scheduled_triggers(channel_identity_id)`,
	} {
		if _, err := sqlDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare legacy schema: %v", err)
		}
	}
	for v := int64(0); v <= 41; v++ {
		if _, err := sqlDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`, v); err != nil {
			t.Fatalf("seed goose version %d: %v", v, err)
		}
	}

	accountID := uuid.New()
	dmChannelID := uuid.New()
	groupChannelID := uuid.New()
	dmIdentityID := uuid.New()
	groupIdentityID := uuid.New()
	dmThreadID := uuid.New()
	groupThreadID := uuid.New()
	now := time.Now().UTC()

	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO channels (id, account_id) VALUES ($1, $2), ($3, $2);
		INSERT INTO threads (id, account_id, deleted_at) VALUES ($4, $2, NULL), ($5, $2, NULL);
		INSERT INTO channel_identities (id, channel_type, platform_subject_id, heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model)
		VALUES
			($6, 'discord', 'user-42', 1, 17, 'discord-model'),
			($7, 'telegram', 'chat-1001', 1, 9, 'group-model');
		INSERT INTO channel_identity_links (id, channel_id, channel_identity_id) VALUES ($8, $1, $6);
		INSERT INTO channel_dm_threads (channel_id, channel_identity_id, thread_id) VALUES ($1, $6, $4);
		INSERT INTO channel_group_threads (channel_id, platform_chat_id, persona_id, thread_id) VALUES ($3, 'chat-1001', NULL, $5);
		INSERT INTO scheduled_triggers (id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES
			($9, $6, 'discord-persona', $2, 'discord-model', 17, $10, $10, $10),
			($11, $7, 'group-persona', $2, 'group-model', 9, $10, $10, $10);`,
		dmChannelID,
		accountID,
		groupChannelID,
		dmThreadID,
		groupThreadID,
		dmIdentityID,
		groupIdentityID,
		uuid.New(),
		uuid.New(),
		now,
		uuid.New(),
	); err != nil {
		t.Fatalf("seed legacy data: %v", err)
	}

	results, err := Up(ctx, db.DSN)
	if err != nil {
		t.Fatalf("upgrade migrations: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("unexpected migration results: %#v", results)
	}

	var (
		enabled  int
		interval int
		model    string
	)
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		  FROM channel_identity_links
		 WHERE channel_id = $1 AND channel_identity_id = $2`,
		dmChannelID, dmIdentityID,
	).Scan(&enabled, &interval, &model); err != nil {
		t.Fatalf("read migrated binding heartbeat config: %v", err)
	}
	if enabled != 1 || interval != 17 || model != "discord-model" {
		t.Fatalf("unexpected binding heartbeat config: enabled=%d interval=%d model=%q", enabled, interval, model)
	}

	var migratedDMChannelID uuid.UUID
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT channel_id FROM scheduled_triggers
		 WHERE channel_identity_id = $1 AND persona_key = 'discord-persona'`,
		dmIdentityID,
	).Scan(&migratedDMChannelID); err != nil {
		t.Fatalf("read migrated dm trigger: %v", err)
	}
	if migratedDMChannelID != dmChannelID {
		t.Fatalf("dm trigger channel_id = %s, want %s", migratedDMChannelID, dmChannelID)
	}

	var migratedGroupChannelID uuid.UUID
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT channel_id FROM scheduled_triggers
		 WHERE channel_identity_id = $1 AND persona_key = 'group-persona'`,
		groupIdentityID,
	).Scan(&migratedGroupChannelID); err != nil {
		t.Fatalf("read migrated group trigger: %v", err)
	}
	if migratedGroupChannelID != groupChannelID {
		t.Fatalf("group trigger channel_id = %s, want %s", migratedGroupChannelID, groupChannelID)
	}
}

func TestUpgradeChannelHeartbeatScopeMigrationDedupesGroupPersonaHistory(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_channel_heartbeat_scope_persona_history")
	ctx := context.Background()

	sqlDB, err := openDB(db.DSN)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	for _, stmt := range []string{
		`CREATE TABLE goose_db_version (
			id SERIAL PRIMARY KEY,
			version_id BIGINT NOT NULL,
			is_applied BOOLEAN NOT NULL,
			tstamp TIMESTAMP DEFAULT now()
		)`,
		`CREATE TABLE personas (
			id UUID PRIMARY KEY,
			account_id UUID NOT NULL,
			key TEXT NOT NULL,
			deleted_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE channel_identities (
			id UUID PRIMARY KEY,
			channel_type TEXT NOT NULL,
			platform_subject_id TEXT NOT NULL,
			heartbeat_enabled INTEGER NOT NULL DEFAULT 0,
			heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30,
			heartbeat_model TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE channels (
			id UUID PRIMARY KEY,
			account_id UUID NOT NULL
		)`,
		`CREATE TABLE threads (
			id UUID PRIMARY KEY,
			account_id UUID NOT NULL,
			created_by_user_id UUID,
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE TABLE channel_dm_threads (
			channel_id UUID NOT NULL,
			channel_identity_id UUID NOT NULL,
			thread_id UUID NOT NULL
		)`,
		`CREATE TABLE channel_group_threads (
			id UUID PRIMARY KEY,
			channel_id UUID NOT NULL,
			platform_chat_id TEXT NOT NULL,
			persona_id UUID NOT NULL,
			thread_id UUID NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (channel_id, platform_chat_id, persona_id),
			UNIQUE (thread_id)
		)`,
		`CREATE TABLE channel_identity_links (
			id UUID PRIMARY KEY,
			channel_id UUID NOT NULL,
			channel_identity_id UUID NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (channel_id, channel_identity_id)
		)`,
		`CREATE TABLE scheduled_triggers (
			id UUID PRIMARY KEY,
			channel_identity_id UUID NOT NULL,
			persona_key TEXT NOT NULL,
			account_id UUID NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			interval_min INT NOT NULL DEFAULT 30,
			next_fire_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX scheduled_triggers_channel_identity_id_idx ON scheduled_triggers(channel_identity_id)`,
	} {
		if _, err := sqlDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare legacy schema: %v", err)
		}
	}
	for v := int64(0); v <= 41; v++ {
		if _, err := sqlDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`, v); err != nil {
			t.Fatalf("seed goose version %d: %v", v, err)
		}
	}

	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	oldPersonaID := uuid.New()
	newPersonaID := uuid.New()
	oldThreadID := uuid.New()
	newThreadID := uuid.New()
	now := time.Now().UTC()

	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO channels (id, account_id) VALUES ($1, $2);
		INSERT INTO channel_identities (id, channel_type, platform_subject_id, heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model)
		VALUES ($3, 'telegram', 'chat-dup', 1, 15, 'group-model');
		INSERT INTO personas (id, account_id, key, deleted_at, created_at)
		VALUES
			($4, $2, 'group-persona', NULL, $7 - interval '2 day'),
			($5, $2, 'group-persona', NULL, $7);
		INSERT INTO threads (id, account_id, created_by_user_id, deleted_at)
		VALUES ($6, $2, NULL, NULL), ($8, $2, NULL, NULL);
		INSERT INTO channel_group_threads (id, channel_id, platform_chat_id, persona_id, thread_id)
		VALUES
			(gen_random_uuid(), $1, 'chat-dup', $4, $6),
			(gen_random_uuid(), $1, 'chat-dup', $5, $8);
		INSERT INTO scheduled_triggers (id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($9, $3, 'group-persona', $2, 'group-model', 15, $7, $7, $7);`,
		channelID,
		accountID,
		identityID,
		oldPersonaID,
		newPersonaID,
		oldThreadID,
		now,
		newThreadID,
		uuid.New(),
	); err != nil {
		t.Fatalf("seed legacy duplicate persona data: %v", err)
	}

	if _, err := Up(ctx, db.DSN); err != nil {
		t.Fatalf("upgrade migrations: %v", err)
	}

	var count int
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM scheduled_triggers
		 WHERE channel_identity_id = $1`,
		identityID,
	).Scan(&count); err != nil {
		t.Fatalf("count migrated triggers: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one migrated trigger, got %d", count)
	}

	var migratedChannelID uuid.UUID
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT channel_id FROM scheduled_triggers
		 WHERE channel_identity_id = $1`,
		identityID,
	).Scan(&migratedChannelID); err != nil {
		t.Fatalf("read migrated trigger channel id: %v", err)
	}
	if migratedChannelID != channelID {
		t.Fatalf("migrated channel_id = %s, want %s", migratedChannelID, channelID)
	}
}

func TestReasoningIterationsBudgetMigration(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_reasoning_budget")
	ctx := context.Background()

	sqlDB, err := openDB(db.DSN)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	provider, err := newProvider(sqlDB)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 85); err != nil {
		t.Fatalf("up to 85: %v", err)
	}

	orgID := uuid.New()
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO orgs (id, slug, name) VALUES ($1, 'migrate-budget-org', 'Migrate Budget Org')`, orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO platform_settings (key, value) VALUES ('limit.agent_max_iterations', '14')`); err != nil {
		t.Fatalf("insert platform setting: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO org_settings (org_id, key, value) VALUES ($1, 'limit.agent_max_iterations', '9')`, orgID); err != nil {
		t.Fatalf("insert org setting: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO personas
			(org_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, executor_type, executor_config_json)
		VALUES ($1, 'legacy-budget', '1', 'Legacy Budget', 'prompt', '{}', '{}', '{"max_iterations":5,"max_output_tokens":1024}'::jsonb, 'agent.simple', '{}'::jsonb)
	`, orgID); err != nil {
		t.Fatalf("insert persona: %v", err)
	}

	result, err := provider.UpByOne(ctx)
	if err != nil {
		t.Fatalf("up by one: %v", err)
	}
	if result == nil || result.Source == nil || result.Source.Version != 86 {
		t.Fatalf("expected migration 86, got %#v", result)
	}

	var platformValue string
	if err := sqlDB.QueryRowContext(ctx, `SELECT value FROM platform_settings WHERE key = 'limit.agent_reasoning_iterations'`).Scan(&platformValue); err != nil {
		t.Fatalf("select renamed platform setting: %v", err)
	}
	if platformValue != "14" {
		t.Fatalf("unexpected platform value: %s", platformValue)
	}
	assertNoSettingRow(t, sqlDB, ctx, `SELECT 1 FROM platform_settings WHERE key = 'limit.agent_max_iterations'`)

	var orgValue string
	if err := sqlDB.QueryRowContext(ctx, `SELECT value FROM org_settings WHERE org_id = $1 AND key = 'limit.agent_reasoning_iterations'`, orgID).Scan(&orgValue); err != nil {
		t.Fatalf("select renamed org setting: %v", err)
	}
	if orgValue != "9" {
		t.Fatalf("unexpected org value: %s", orgValue)
	}
	assertNoSettingRow(t, sqlDB, ctx, `SELECT 1 FROM org_settings WHERE org_id = '`+orgID.String()+`' AND key = 'limit.agent_max_iterations'`)

	var budgetsJSON string
	if err := sqlDB.QueryRowContext(ctx, `SELECT budgets_json::text FROM personas WHERE persona_key = 'legacy-budget'`).Scan(&budgetsJSON); err != nil {
		t.Fatalf("select persona budgets: %v", err)
	}
	if budgetsJSON != `{"max_output_tokens": 1024, "reasoning_iterations": 5}` && budgetsJSON != `{"reasoning_iterations": 5, "max_output_tokens": 1024}` {
		t.Fatalf("unexpected budgets_json: %s", budgetsJSON)
	}
}

func assertNoSettingRow(t *testing.T, db *sql.DB, ctx context.Context, query string) {
	t.Helper()
	var exists int
	err := db.QueryRowContext(ctx, query).Scan(&exists)
	if err == nil {
		t.Fatalf("expected no row for query %s", query)
	}
	if err != sql.ErrNoRows {
		t.Fatalf("unexpected error for query %s: %v", query, err)
	}
}

func TestLlmRoutesProviderModelsMigration(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_llm_provider_models")
	ctx := context.Background()

	sqlDB, err := openDB(db.DSN)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	provider, err := newProvider(sqlDB)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 90); err != nil {
		t.Fatalf("up to 90: %v", err)
	}

	orgID := uuid.New()
	credID := uuid.New()
	keepDefaultID := uuid.New()
	dupID := uuid.New()
	otherDefaultID := uuid.New()
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO orgs (id, slug, name) VALUES ($1, 'migrate-llm-provider-models', 'Migrate LLM Provider Models')`, orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO llm_credentials (id, org_id, provider, name, advanced_json)
		VALUES ($1, $2, 'openai', 'provider-a', '{}'::jsonb)
	`, credID, orgID); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO llm_routes (id, org_id, credential_id, model, priority, is_default, when_json, created_at)
		VALUES
			($1, $4, $5, 'gpt-4o', 5, TRUE, '{}'::jsonb, '2026-03-01T00:00:00Z'::timestamptz),
			($2, $4, $5, 'GPT-4O', 1, FALSE, '{}'::jsonb, '2026-03-02T00:00:00Z'::timestamptz),
			($3, $4, $5, 'claude-3-5-sonnet', 9, TRUE, '{}'::jsonb, '2026-03-03T00:00:00Z'::timestamptz)
	`, keepDefaultID, dupID, otherDefaultID, orgID, credID); err != nil {
		t.Fatalf("insert routes: %v", err)
	}

	result, err := provider.UpByOne(ctx)
	if err != nil {
		t.Fatalf("up by one: %v", err)
	}
	if result == nil || result.Source == nil || result.Source.Version != 91 {
		t.Fatalf("expected migration 91, got %#v", result)
	}

	var exists bool
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'llm_routes' AND column_name = 'tags'
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("check tags column: %v", err)
	}
	if !exists {
		t.Fatal("expected llm_routes.tags column")
	}

	var remaining int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_routes WHERE credential_id = $1`, credID).Scan(&remaining); err != nil {
		t.Fatalf("count routes: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("expected 2 routes after dedupe, got %d", remaining)
	}

	var keepDefault bool
	if err := sqlDB.QueryRowContext(ctx, `SELECT is_default FROM llm_routes WHERE id = $1`, keepDefaultID).Scan(&keepDefault); err != nil {
		t.Fatalf("select keep default: %v", err)
	}
	if keepDefault {
		t.Fatal("expected lower-priority default to be cleared")
	}

	var otherDefault bool
	if err := sqlDB.QueryRowContext(ctx, `SELECT is_default FROM llm_routes WHERE id = $1`, otherDefaultID).Scan(&otherDefault); err != nil {
		t.Fatalf("select other default: %v", err)
	}
	if !otherDefault {
		t.Fatal("expected highest-priority default to remain default")
	}

	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO llm_routes (org_id, credential_id, model, priority, is_default, tags, when_json)
		VALUES ($1, $2, 'Gpt-4O', 0, FALSE, '{}'::text[], '{}'::jsonb)
	`, orgID, credID); err == nil {
		t.Fatal("expected unique constraint on credential/model")
	}

	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO llm_routes (org_id, credential_id, model, priority, is_default, tags, when_json)
		VALUES ($1, $2, 'gpt-4.1', 0, TRUE, '{}'::text[], '{}'::jsonb)
	`, orgID, credID); err == nil {
		t.Fatal("expected unique constraint on credential default route")
	}
}
