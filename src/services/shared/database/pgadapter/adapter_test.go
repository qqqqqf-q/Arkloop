//go:build !desktop

package pgadapter

import (
	"errors"
	"testing"

	"arkloop/services/shared/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// Interface satisfaction (compile-time checks)
// ---------------------------------------------------------------------------

var _ database.DB = (*Pool)(nil)
var _ database.Tx = (*tx)(nil)
var _ database.Row = (*row)(nil)
var _ database.Rows = (*rows)(nil)
var _ database.Result = result{}

// ---------------------------------------------------------------------------
// translateError
// ---------------------------------------------------------------------------

func TestTranslateError_Nil(t *testing.T) {
	t.Parallel()
	if err := translateError(nil); err != nil {
		t.Errorf("translateError(nil) = %v; want nil", err)
	}
}

func TestTranslateError_ErrNoRows(t *testing.T) {
	t.Parallel()
	err := translateError(pgx.ErrNoRows)

	if !errors.Is(err, database.ErrNoRows) {
		t.Errorf("errors.Is(err, database.ErrNoRows) = false; want true")
	}
	if !database.IsNoRows(err) {
		t.Errorf("database.IsNoRows(err) = false; want true")
	}
	// The original error text should be preserved somewhere in the chain.
	if err.Error() == database.ErrNoRows.Error() {
		t.Errorf("wrapped error lost original context; error strings are identical")
	}
}

func TestTranslateError_PgError(t *testing.T) {
	t.Parallel()
	pgErr := &pgconn.PgError{
		Code:    "23505",
		Message: "unique_violation",
	}
	err := translateError(pgErr)

	var driverErr *database.DriverError
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *database.DriverError; got %T", err)
	}
	if driverErr.Driver != "pgx" {
		t.Errorf("DriverError.Driver = %q; want %q", driverErr.Driver, "pgx")
	}
	// The original PgError should still be reachable via Unwrap.
	var unwrappedPg *pgconn.PgError
	if !errors.As(err, &unwrappedPg) {
		t.Error("cannot unwrap to *pgconn.PgError through DriverError")
	}
	if !database.IsDriverError(err) {
		t.Error("database.IsDriverError(err) = false; want true")
	}
}

func TestTranslateError_OtherError(t *testing.T) {
	t.Parallel()
	orig := errors.New("something else")
	err := translateError(orig)

	if err != orig {
		t.Errorf("expected original error to pass through; got %v", err)
	}
	if database.IsDriverError(err) {
		t.Error("non-pg error should not be a DriverError")
	}
}

// ---------------------------------------------------------------------------
// Dialect (PostgresDialect via Dialect())
// ---------------------------------------------------------------------------

func TestDialect_Name(t *testing.T) {
	t.Parallel()
	d := Dialect()
	if d.Name() != database.DialectPostgres {
		t.Errorf("Name() = %q; want %q", d.Name(), database.DialectPostgres)
	}
}

func TestDialect_Placeholder(t *testing.T) {
	t.Parallel()
	d := Dialect()
	tests := []struct {
		index int
		want  string
	}{
		{1, "$1"},
		{3, "$3"},
		{10, "$10"},
	}
	for _, tt := range tests {
		if got := d.Placeholder(tt.index); got != tt.want {
			t.Errorf("Placeholder(%d) = %q; want %q", tt.index, got, tt.want)
		}
	}
}

func TestDialect_Now(t *testing.T) {
	t.Parallel()
	d := Dialect()
	if got := d.Now(); got != "now()" {
		t.Errorf("Now() = %q; want %q", got, "now()")
	}
}

func TestDialect_IntervalAdd(t *testing.T) {
	t.Parallel()
	d := Dialect()
	got := d.IntervalAdd("created_at", "24 hours", "+24 hours")
	want := "created_at + INTERVAL '24 hours'"
	if got != want {
		t.Errorf("IntervalAdd() = %q; want %q", got, want)
	}
}

func TestDialect_JSONCast(t *testing.T) {
	t.Parallel()
	d := Dialect()
	got := d.JSONCast("data")
	if got != "data::jsonb" {
		t.Errorf("JSONCast() = %q; want %q", got, "data::jsonb")
	}
}

func TestDialect_ForUpdate(t *testing.T) {
	t.Parallel()
	d := Dialect()
	if got := d.ForUpdate(); got != "FOR UPDATE" {
		t.Errorf("ForUpdate() = %q; want %q", got, "FOR UPDATE")
	}
}

func TestDialect_ILike(t *testing.T) {
	t.Parallel()
	d := Dialect()
	if got := d.ILike(); got != "ILIKE" {
		t.Errorf("ILike() = %q; want %q", got, "ILIKE")
	}
}

func TestDialect_ArrayAny(t *testing.T) {
	t.Parallel()
	d := Dialect()
	got := d.ArrayAny("status", 2)
	want := "status = ANY($2)"
	if got != want {
		t.Errorf("ArrayAny() = %q; want %q", got, want)
	}
}

func TestDialect_OnConflict(t *testing.T) {
	t.Parallel()
	d := Dialect()

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

func TestDialect_Returning(t *testing.T) {
	t.Parallel()
	d := Dialect()
	got := d.Returning("id, name")
	want := "RETURNING id, name"
	if got != want {
		t.Errorf("Returning() = %q; want %q", got, want)
	}
}

func TestDialect_Sequence(t *testing.T) {
	t.Parallel()
	d := Dialect()
	got := d.Sequence("my_seq")
	want := "nextval('my_seq')"
	if got != want {
		t.Errorf("Sequence() = %q; want %q", got, want)
	}
}
