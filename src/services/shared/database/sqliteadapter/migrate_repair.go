//go:build desktop

package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
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
		// 00086 replaced plan_mode with collaboration_mode during active development.
		// Local desktop databases may already have 00086 marked applied with the old shape.
		{Table: "threads", Column: "collaboration_mode", Definition: "TEXT NOT NULL DEFAULT 'default' CHECK (collaboration_mode IN ('default', 'plan'))"},
		{Table: "threads", Column: "collaboration_mode_revision", Definition: "INTEGER NOT NULL DEFAULT 0"},
		// 00043/00090 may already be marked applied on local desktop databases.
		{Table: "channels", Column: "owner_user_id", Definition: "TEXT REFERENCES users(id)"},
		{Table: "api_keys", Column: "scopes", Definition: "TEXT NOT NULL DEFAULT '[]'"},
		{Table: "api_keys", Column: "revoked_at", Definition: "TEXT"},
		{Table: "secrets", Column: "owner_kind", Definition: "TEXT NOT NULL DEFAULT 'platform' CHECK (owner_kind IN ('platform', 'user'))"},
		{Table: "secrets", Column: "owner_user_id", Definition: "TEXT REFERENCES users(id) ON DELETE CASCADE"},
		{Table: "secrets", Column: "rotated_at", Definition: "TEXT"},
	}

	for _, s := range specs {
		tableExists, err := sqliteTableExists(ctx, db, s.Table)
		if err != nil {
			return fmt.Errorf("repairMissingColumns: check table %s: %w", s.Table, err)
		}
		if !tableExists {
			continue
		}
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
	if err := repairThreadCollaborationModeValues(ctx, db); err != nil {
		return err
	}
	if err := repairSecretsAccountDefault(ctx, db); err != nil {
		return err
	}
	return nil
}

func repairThreadCollaborationModeValues(ctx context.Context, db *sql.DB) error {
	columns, err := sqliteTableColumns(ctx, db, "threads")
	if err != nil {
		return fmt.Errorf("repairThreadCollaborationModeValues: load threads columns: %w", err)
	}
	if !hasSQLiteColumns(columns, "plan_mode", "collaboration_mode") {
		return nil
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE threads
		   SET collaboration_mode = CASE WHEN plan_mode THEN 'plan' ELSE 'default' END
		 WHERE collaboration_mode = 'default'
	`); err != nil {
		return fmt.Errorf("repairThreadCollaborationModeValues: migrate plan_mode values: %w", err)
	}
	return nil
}

func repairSecretsAccountDefault(ctx context.Context, db *sql.DB) error {
	exists, err := sqliteTableExists(ctx, db, "secrets")
	if err != nil {
		return fmt.Errorf("repairSecretsAccountDefault: check secrets table: %w", err)
	}
	if !exists {
		return nil
	}

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(secrets)`)
	if err != nil {
		return fmt.Errorf("repairSecretsAccountDefault: pragma secrets: %w", err)
	}
	defer rows.Close()

	accountDefault := ""
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue any
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("repairSecretsAccountDefault: scan secrets columns: %w", err)
		}
		if name != "account_id" {
			continue
		}
		if defaultValue != nil {
			accountDefault = fmt.Sprint(defaultValue)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("repairSecretsAccountDefault: read secrets columns: %w", err)
	}
	if strings.Contains(accountDefault, "00000000-0000-4000-8000-000000000002") {
		return nil
	}

	stmts := []string{
		`PRAGMA foreign_keys = OFF`,
		`PRAGMA legacy_alter_table = ON`,
		`DROP INDEX IF EXISTS secrets_platform_name_idx`,
		`DROP INDEX IF EXISTS secrets_user_name_idx`,
		`ALTER TABLE secrets RENAME TO secrets_repair_missing_default`,
		`CREATE TABLE secrets (
			id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			account_id      TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
			owner_kind      TEXT NOT NULL DEFAULT 'platform',
			owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE,
			name            TEXT NOT NULL,
			encrypted_value TEXT NOT NULL,
			key_version     INTEGER NOT NULL DEFAULT 1,
			created_at      TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
			rotated_at      TEXT
		)`,
		`INSERT INTO secrets (id, account_id, owner_kind, owner_user_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at)
		 SELECT id,
		        COALESCE(NULLIF(account_id, ''), '00000000-0000-4000-8000-000000000002'),
		        COALESCE(NULLIF(owner_kind, ''), 'platform'),
		        owner_user_id,
		        name,
		        encrypted_value,
		        key_version,
		        created_at,
		        updated_at,
		        rotated_at
		   FROM secrets_repair_missing_default`,
		`DROP TABLE secrets_repair_missing_default`,
		`CREATE UNIQUE INDEX IF NOT EXISTS secrets_platform_name_idx
			ON secrets (name)
			WHERE owner_kind = 'platform'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS secrets_user_name_idx
			ON secrets (owner_user_id, name)
			WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL`,
		`PRAGMA legacy_alter_table = OFF`,
		`PRAGMA foreign_keys = ON`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("repairSecretsAccountDefault: %s: %w", stmt, err)
		}
	}
	slog.InfoContext(ctx, "sqliteadapter: repaired secrets account default")
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
