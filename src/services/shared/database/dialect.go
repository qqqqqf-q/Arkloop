package database

import "fmt"

// Dialect identifies the SQL dialect of a database backend.
type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectSQLite   Dialect = "sqlite"
)

// DialectHelper generates dialect-specific SQL fragments.
// Each database adapter provides its own implementation.
type DialectHelper interface {
	// Name returns the dialect identifier.
	Name() Dialect

	// Placeholder returns the parameter placeholder for the given 1-based index.
	// PostgreSQL: $1, $2, ...  SQLite: ?1, ?2, ...
	Placeholder(index int) string

	// Returning wraps a RETURNING clause.
	// PostgreSQL: "RETURNING id, name"  SQLite: "RETURNING id, name" (3.35+)
	Returning(columns string) string

	// Now returns the expression for the current timestamp.
	// PostgreSQL: now()  SQLite: datetime('now')
	Now() string

	// IntervalAdd returns an expression that adds a duration to a timestamp expression.
	// PostgreSQL: expr + INTERVAL '24 hours'  SQLite: datetime(expr, '+24 hours')
	//
	// TODO(m5): The current signature leaks both dialect formats to the caller.
	// Consider IntervalAdd(expr string, duration time.Duration) string and let
	// each dialect format internally. Blocked on updating all callers + tests.
	IntervalAdd(expr string, pgInterval string, sqliteModifier string) string

	// JSONCast wraps an expression with a JSON type cast.
	// PostgreSQL: expr::jsonb  SQLite: expr (no cast needed, stored as TEXT)
	JSONCast(expr string) string

	// OnConflictDoUpdate returns an upsert clause.
	// PostgreSQL: ON CONFLICT (cols) DO UPDATE SET ...
	// SQLite: ON CONFLICT (cols) DO UPDATE SET ...  (syntax is compatible)
	OnConflictDoUpdate(conflictColumns string, setClauses string) string

	// OnConflictDoNothing returns a conflict-ignore clause.
	OnConflictDoNothing(conflictColumns string) string

	// Sequence returns an expression to allocate the next value from a named sequence.
	// PostgreSQL: nextval('seq_name')  SQLite: must use a different approach
	Sequence(name string) string

	// ForUpdate returns a row-locking clause (empty string if not supported).
	ForUpdate() string

	// ILike returns the case-insensitive LIKE operator.
	// PostgreSQL: ILIKE  SQLite: LIKE (case-insensitive for ASCII by default)
	ILike() string

	// ArrayAny returns an expression for "column = ANY(placeholder)".
	// PostgreSQL: column = ANY($n)  SQLite: must expand to IN (?, ?, ...)
	ArrayAny(column string, placeholderIndex int) string
}

// PostgresDialect implements DialectHelper for PostgreSQL.
type PostgresDialect struct{}

func (PostgresDialect) Name() Dialect                    { return DialectPostgres }
func (PostgresDialect) Placeholder(index int) string     { return fmt.Sprintf("$%d", index) }
func (PostgresDialect) Returning(columns string) string  { return "RETURNING " + columns }
func (PostgresDialect) Now() string                      { return "now()" }
func (PostgresDialect) JSONCast(expr string) string      { return expr + "::jsonb" }
func (PostgresDialect) ForUpdate() string                { return "FOR UPDATE" }
func (PostgresDialect) ILike() string                    { return "ILIKE" }
func (PostgresDialect) Sequence(name string) string      { return fmt.Sprintf("nextval('%s')", name) }

func (PostgresDialect) IntervalAdd(expr string, pgInterval string, _ string) string {
	return expr + " + INTERVAL '" + pgInterval + "'"
}

func (PostgresDialect) OnConflictDoUpdate(conflictColumns string, setClauses string) string {
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s", conflictColumns, setClauses)
}

func (PostgresDialect) OnConflictDoNothing(conflictColumns string) string {
	return fmt.Sprintf("ON CONFLICT (%s) DO NOTHING", conflictColumns)
}

func (PostgresDialect) ArrayAny(column string, placeholderIndex int) string {
	return fmt.Sprintf("%s = ANY($%d)", column, placeholderIndex)
}
