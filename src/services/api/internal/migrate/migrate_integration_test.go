package migrate

import (
	"context"
	"database/sql"
	"testing"

	"arkloop/services/api/internal/testutil"
)

func TestUpFromScratch(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "migrate_up")
	ctx := context.Background()

	results, err := Up(ctx, db.DSN)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	// 45 migrations (00001..00045)
	if len(results) != 45 {
		t.Fatalf("expected 45 migrations, got %d", len(results))
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
	// DownOne 从 45 回退到 44
	const prevVersion int64 = 44
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
