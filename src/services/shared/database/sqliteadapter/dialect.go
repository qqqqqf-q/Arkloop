//go:build desktop

package sqliteadapter

import (
	"fmt"

	"arkloop/services/shared/database"
)

// SQLiteDialect implements DialectHelper for SQLite.
type SQLiteDialect struct{}

func (SQLiteDialect) Name() database.Dialect        { return database.DialectSQLite }
func (SQLiteDialect) Placeholder(index int) string   { return fmt.Sprintf("?%d", index) }
func (SQLiteDialect) Returning(columns string) string { return "RETURNING " + columns }
func (SQLiteDialect) Now() string                    { return "datetime('now')" }
func (SQLiteDialect) ForUpdate() string              { return "" }
func (SQLiteDialect) ILike() string                  { return "LIKE" }
func (SQLiteDialect) JSONCast(expr string) string    { return expr }

func (SQLiteDialect) IntervalAdd(expr string, _ string, sqliteModifier string) string {
	return fmt.Sprintf("datetime(%s, '%s')", expr, sqliteModifier)
}

func (SQLiteDialect) OnConflictDoUpdate(conflictColumns string, setClauses string) string {
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s", conflictColumns, setClauses)
}

func (SQLiteDialect) OnConflictDoNothing(conflictColumns string) string {
	return fmt.Sprintf("ON CONFLICT (%s) DO NOTHING", conflictColumns)
}

// Sequence uses a helper table approach for SQLite.
// The caller must ensure a `_sequences` table exists.
func (SQLiteDialect) Sequence(name string) string {
	return fmt.Sprintf("(UPDATE _sequences SET val = val + 1 WHERE name = '%s' RETURNING val)", name)
}

// ArrayAny for SQLite uses json_each to check membership.
// The parameter should be a JSON array string, e.g. '["a","b"]'.
func (SQLiteDialect) ArrayAny(column string, placeholderIndex int) string {
	return fmt.Sprintf("EXISTS(SELECT 1 FROM json_each(?%d) WHERE value = %s)", placeholderIndex, column)
}

// Dialect returns an SQLiteDialect instance.
func Dialect() database.DialectHelper {
	return SQLiteDialect{}
}
