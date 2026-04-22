//go:build desktop

package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// repairColumnSpec describes a column that should exist on a table.
type repairColumnSpec struct {
	Table      string
	Column     string
	Definition string // e.g. "TEXT", "INTEGER NOT NULL DEFAULT 0"
}

// repairMissingColumns adds columns that should have been created by earlier
// migrations but may be absent due to partial applies. Each ADD COLUMN is
// guarded by a PRAGMA table_info check so it is safe to run repeatedly.
func repairMissingColumns(ctx context.Context, db *sql.DB) error {
	specs := []repairColumnSpec{
		// 00083 should have added these to scheduled_triggers.
		{Table: "scheduled_triggers", Column: "last_user_msg_at", Definition: "TEXT"},
		{Table: "scheduled_triggers", Column: "burst_start_at", Definition: "TEXT"},
	}

	for _, s := range specs {
		exists, err := columnExists(ctx, db, s.Table, s.Column)
		if err != nil {
			return fmt.Errorf("repairMissingColumns: check %s.%s: %w", s.Table, s.Column, err)
		}
		if exists {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", s.Table, s.Column, s.Definition)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("repairMissingColumns: %s: %w", stmt, err)
		}
		slog.InfoContext(ctx, "sqliteadapter: repaired missing column", "table", s.Table, "column", s.Column)
	}
	return nil
}

// columnExists returns true if the given column is present on the table.
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
