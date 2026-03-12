//go:build desktop

package sqliteadapter

import (
	"context"
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

	// Verify that at least one application table exists (orgs from migration 1).
	// orgs is kept in desktop mode as a workspace container; many tables have org_id FKs.
	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='orgs'`).Scan(&count)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if count != 1 {
		t.Fatalf("orgs table not found after auto-migrate")
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
