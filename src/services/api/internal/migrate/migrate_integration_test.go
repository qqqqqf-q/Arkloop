package migrate

import (
	"context"
	"database/sql"
	"testing"

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
	if int64(len(results)) != ExpectedVersion {
		t.Fatalf("expected %d migrations, got %d", ExpectedVersion, len(results))
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
	defer conn.Close()

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

func TestReasoningIterationsBudgetMigration(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_reasoning_budget")
	ctx := context.Background()

	sqlDB, err := openDB(db.DSN)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

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
