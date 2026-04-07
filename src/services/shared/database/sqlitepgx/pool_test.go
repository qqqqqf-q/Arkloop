//go:build desktop

package sqlitepgx

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func openTestDB(t *testing.T) *Pool {
	t.Helper()
	pool, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func createTestTable(t *testing.T, pool *Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Pool / Open
// ---------------------------------------------------------------------------

func TestOpen(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestUnwrap(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	if pool.Unwrap() == nil {
		t.Fatal("Unwrap() returned nil")
	}
}

// ---------------------------------------------------------------------------
// Exec
// ---------------------------------------------------------------------------

func TestExec_Insert(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)
	ctx := context.Background()

	tag, err := pool.Exec(ctx, `INSERT INTO items (id, name) VALUES (1, 'alpha')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Errorf("RowsAffected() = %d; want 1", tag.RowsAffected())
	}
}

// ---------------------------------------------------------------------------
// Query
// ---------------------------------------------------------------------------

func TestQuery_Select(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := pool.Exec(ctx, `INSERT INTO items (name) VALUES (?)`, name); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}

	rows, err := pool.Query(ctx, `SELECT id, name FROM items ORDER BY name`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("got %d rows; want 3", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("got %v; want [alpha beta gamma]", names)
	}
}

// ---------------------------------------------------------------------------
// QueryRow
// ---------------------------------------------------------------------------

func TestQueryRow_Scan(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO items (id, name) VALUES (1, 'alpha')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var id int
	var name string
	if err := pool.QueryRow(ctx, `SELECT id, name FROM items WHERE id = 1`).Scan(&id, &name); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if id != 1 || name != "alpha" {
		t.Errorf("got id=%d name=%q; want id=1 name=alpha", id, name)
	}
}

func TestQueryRow_ErrNoRows(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)

	var id int
	err := pool.QueryRow(context.Background(), `SELECT id FROM items WHERE id = 999`).Scan(&id)
	if err == nil {
		t.Fatal("expected error for missing row, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows; got %v", err)
	}
}

func TestQueryRow_ScanUUIDPointer(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `CREATE TABLE uuids (id TEXT PRIMARY KEY, parent_id TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	id := uuid.New()
	parentID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO uuids (id, parent_id) VALUES (?, ?)`, id.String(), parentID.String()); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	var (
		gotID     uuid.UUID
		gotParent *uuid.UUID
	)
	if err := pool.QueryRow(ctx, `SELECT id, parent_id FROM uuids WHERE id = ?`, id.String()).Scan(&gotID, &gotParent); err != nil {
		t.Fatalf("scan uuid row: %v", err)
	}
	if gotID != id {
		t.Fatalf("id = %s, want %s", gotID, id)
	}
	if gotParent == nil || *gotParent != parentID {
		t.Fatalf("parent_id = %#v, want %s", gotParent, parentID)
	}
}

// ---------------------------------------------------------------------------
// SQL Preprocessing
// ---------------------------------------------------------------------------

func TestRewriteSQL_Now(t *testing.T) {
	t.Parallel()
	got := rewriteSQL("SELECT now()")
	want := "SELECT datetime('now')"
	if got != want {
		t.Errorf("rewriteSQL(now()) = %q; want %q", got, want)
	}
}

func TestRewriteSQL_ILIKE(t *testing.T) {
	t.Parallel()
	got := rewriteSQL("WHERE name ILIKE $1")
	want := "WHERE name LIKE $1"
	if got != want {
		t.Errorf("rewriteSQL(ILIKE) = %q; want %q", got, want)
	}
}

func TestRewriteSQL_TypeCast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"col::jsonb", "col"},
		{"col::json", "col"},
		{"col::text", "col"},
		{"col::uuid", "col"},
		{"col::integer", "col"},
		{"col::bigint", "col"},
		{"col::boolean", "col"},
		{"col::inet", "col"},
		{"col::uuid[]", "col"},
		{"col::text[]", "col"},
	}
	for _, tt := range tests {
		got := rewriteSQL(tt.input)
		if got != tt.want {
			t.Errorf("rewriteSQL(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestRewriteSQL_JSONBSet(t *testing.T) {
	t.Parallel()
	got := rewriteSQL("UPDATE profile_registries SET metadata_json = jsonb_set(COALESCE(metadata_json, '{}'::jsonb), '{installed_skill_refs}', $1::jsonb, true)")
	want := "UPDATE profile_registries SET metadata_json = json_set(COALESCE(metadata_json, '{}'), '$.installed_skill_refs', json($1))"
	if got != want {
		t.Errorf("rewriteSQL(jsonb_set) = %q; want %q", got, want)
	}
}

func TestRewriteSQL_Interval(t *testing.T) {
	t.Parallel()
	got := rewriteSQL("datetime(created_at, interval '30 days')")
	if got == "datetime(created_at, interval '30 days')" {
		t.Errorf("interval was not rewritten: %q", got)
	}
}

func TestRewriteSQL_Passthrough(t *testing.T) {
	t.Parallel()
	plain := "SELECT id, name FROM items WHERE id = $1"
	got := rewriteSQL(plain)
	if got != plain {
		t.Errorf("plain SQL was modified: %q -> %q", plain, got)
	}
}

func TestExpandAnyArgs_UUIDSlice(t *testing.T) {
	t.Parallel()
	id1 := uuid.New()
	id2 := uuid.New()
	sql, args := expandAnyArgs(`SELECT id FROM items WHERE id = ANY($1::uuid[]) AND owner_id = $2`, []any{[]uuid.UUID{id1, id2}, "owner-1"})
	if sql != `SELECT id FROM items WHERE id IN ($1, $2) AND owner_id = $3` {
		t.Fatalf("unexpected sql rewrite: %q", sql)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	if args[0] != id1.String() || args[1] != id2.String() || args[2] != "owner-1" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestConvertArg_UUIDSlice(t *testing.T) {
	t.Parallel()
	id1 := uuid.New()
	id2 := uuid.New()
	got := convertArg([]uuid.UUID{id1, id2})
	text, ok := got.(string)
	if !ok {
		t.Fatalf("expected string result, got %#v", got)
	}
	if text != `["`+id1.String()+`","`+id2.String()+`"]` {
		t.Fatalf("unexpected converted arg: %s", text)
	}
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

func TestTransaction_Commit(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO items (id, name) VALUES (1, 'alpha')`); err != nil {
		t.Fatalf("exec in tx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM items WHERE id = 1`).Scan(&name); err != nil {
		t.Fatalf("select after commit: %v", err)
	}
	if name != "alpha" {
		t.Errorf("name = %q; want alpha", name)
	}
}

func TestTransaction_Rollback(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO items (id, name) VALUES (1, 'alpha')`); err != nil {
		t.Fatalf("exec in tx: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&count); err != nil {
		t.Fatalf("select after rollback: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d; want 0 after rollback", count)
	}
}

func TestTransaction_QueryRow_ErrNoRows(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var id int
	err = tx.QueryRow(ctx, `SELECT id FROM items WHERE id = 999`).Scan(&id)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows in tx; got %v", err)
	}
}

func TestTransaction_Query(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO items (id, name) VALUES (1, 'alpha')`); err != nil {
		t.Fatalf("insert in tx: %v", err)
	}

	rows, err := tx.Query(ctx, `SELECT name FROM items`)
	if err != nil {
		t.Fatalf("query in tx: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected one row in tx query")
	}
	var name string
	if err := rows.Scan(&name); err != nil {
		t.Fatalf("scan in tx: %v", err)
	}
	if name != "alpha" {
		t.Errorf("name = %q; want alpha", name)
	}

	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
