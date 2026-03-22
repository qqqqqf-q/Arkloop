//go:build desktop

package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	desktopCompatUserID    = "00000000-0000-4000-8000-000000000001"
	desktopCompatAccountID = "00000000-0000-4000-8000-000000000002"
)

// repairSchemasPreMigration fixes schema issues that must be resolved before
// goose migrations run, to prevent migrations from failing mid-execution.
func repairSchemasPreMigration(ctx context.Context, db *sql.DB) error {
	return repairScheduledTriggersSchema(ctx, db)
}

func repairLegacySchemas(ctx context.Context, db *sql.DB) error {
	if err := repairLegacySecretsSchema(ctx, db); err != nil {
		return err
	}
	if err := repairHeartbeatPersonaColumns(ctx, db); err != nil {
		return err
	}
	needsChannelRepair, err := channelsNeedSecretsReferenceRepair(ctx, db)
	if err != nil {
		return err
	}
	if !needsChannelRepair {
		return nil
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("sqliteadapter: disable foreign keys for channel repair: %w", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	}()
	if err := rebuildChannelTablesForSecretsRepair(ctx, db); err != nil {
		return err
	}
	return nil
}

func repairLegacySecretsSchema(ctx context.Context, db *sql.DB) error {
	exists, err := sqliteTableExists(ctx, db, "secrets")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	columns, err := sqliteTableColumns(ctx, db, "secrets")
	if err != nil {
		return err
	}
	if hasSQLiteColumns(columns, "id", "account_id", "owner_kind", "owner_user_id", "name", "encrypted_value", "key_version", "created_at", "updated_at", "rotated_at") {
		return nil
	}
	if !hasSQLiteColumns(columns, "id", "account_id", "name", "encrypted_value", "key_version", "created_at", "updated_at") {
		return fmt.Errorf("sqliteadapter: secrets schema is incompatible with desktop channel support")
	}

	hasOwnerKind := hasSQLiteColumns(columns, "owner_kind")
	hasOwnerUserID := hasSQLiteColumns(columns, "owner_user_id")
	hasRotatedAt := hasSQLiteColumns(columns, "rotated_at")
	accountIDDefault, hasAccountIDDefault, err := sqliteTableColumnDefault(ctx, db, "secrets", "account_id")
	if err != nil {
		return err
	}
	hasDesktopAccountDefault := hasAccountIDDefault && strings.Contains(accountIDDefault, desktopCompatAccountID)
	if hasOwnerKind && hasOwnerUserID && hasRotatedAt && hasDesktopAccountDefault {
		return nil
	}

	ownerKind := "platform"
	var ownerUserID any
	hasDesktopUser, err := sqliteRowExists(ctx, db, `SELECT 1 FROM users WHERE id = ? LIMIT 1`, desktopCompatUserID)
	if err != nil {
		return err
	}
	if hasDesktopUser {
		ownerKind = "user"
		ownerUserID = desktopCompatUserID
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("sqliteadapter: disable foreign keys for secrets repair: %w", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	}()

	if err := rebuildSecretsTable(ctx, db, hasOwnerKind, hasOwnerUserID, hasRotatedAt, ownerKind, ownerUserID); err != nil {
		return err
	}
	if err := rebuildChannelTablesForSecretsRepair(ctx, db); err != nil {
		return err
	}
	return nil
}

func rebuildSecretsTable(ctx context.Context, db *sql.DB, hasOwnerKind, hasOwnerUserID, hasRotatedAt bool, ownerKind string, ownerUserID any) error {
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS secrets_platform_name_idx`,
		`DROP INDEX IF EXISTS secrets_user_name_idx`,
		`DROP TABLE IF EXISTS secrets_legacy_compat_00029`,
		`ALTER TABLE secrets RENAME TO secrets_legacy_compat_00029`,
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
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqliteadapter: rebuild secrets schema: %w", err)
		}
	}

	insertSQL := `INSERT INTO secrets (
			id,
			account_id,
			owner_kind,
			owner_user_id,
			name,
			encrypted_value,
			key_version,
			created_at,
			updated_at,
			rotated_at
		)
		SELECT
			id,
			COALESCE(account_id, ?),
			?,
			?,
			name,
			encrypted_value,
			COALESCE(key_version, 1),
			COALESCE(created_at, datetime('now')),
			COALESCE(updated_at, COALESCE(created_at, datetime('now'))),
			NULL
		FROM secrets_legacy_compat_00029`
	args := []any{desktopCompatAccountID, ownerKind, ownerUserID}
	if hasOwnerKind && hasOwnerUserID {
		rotatedExpr := "NULL"
		if hasRotatedAt {
			rotatedExpr = "rotated_at"
		}
		insertSQL = `INSERT INTO secrets (
				id,
				account_id,
				owner_kind,
				owner_user_id,
				name,
				encrypted_value,
				key_version,
				created_at,
				updated_at,
				rotated_at
			)
			SELECT
				id,
				COALESCE(account_id, ?),
				COALESCE(owner_kind, 'platform'),
				owner_user_id,
				name,
				encrypted_value,
				COALESCE(key_version, 1),
				COALESCE(created_at, datetime('now')),
				COALESCE(updated_at, COALESCE(created_at, datetime('now'))),
				` + rotatedExpr + `
			FROM secrets_legacy_compat_00029`
		args = []any{desktopCompatAccountID}
	}
	if _, err := db.ExecContext(ctx, insertSQL, args...); err != nil {
		return fmt.Errorf("sqliteadapter: migrate secrets rows: %w", err)
	}

	for _, stmt := range []string{
		`CREATE UNIQUE INDEX secrets_platform_name_idx
			ON secrets (name)
			WHERE owner_kind = 'platform'`,
		`CREATE UNIQUE INDEX secrets_user_name_idx
			ON secrets (owner_user_id, name)
			WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL`,
		`DROP TABLE secrets_legacy_compat_00029`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqliteadapter: finalize secrets rebuild: %w", err)
		}
	}

	return nil
}

func rebuildChannelTablesForSecretsRepair(ctx context.Context, db *sql.DB) error {
	channelsExists, err := sqliteTableExists(ctx, db, "channels")
	if err != nil {
		return err
	}
	if !channelsExists {
		return nil
	}

	dmThreadsExists, err := sqliteTableExists(ctx, db, "channel_dm_threads")
	if err != nil {
		return err
	}
	receiptsExists, err := sqliteTableExists(ctx, db, "channel_message_receipts")
	if err != nil {
		return err
	}

	for _, stmt := range []string{
		`DROP TABLE IF EXISTS channel_message_receipts_legacy_compat_00029`,
		`DROP TABLE IF EXISTS channel_dm_threads_legacy_compat_00029`,
		`DROP TABLE IF EXISTS channels_legacy_compat_00029`,
		`DROP INDEX IF EXISTS idx_channel_message_receipts_channel_id`,
		`DROP INDEX IF EXISTS idx_channel_dm_threads_channel_id`,
		`DROP INDEX IF EXISTS idx_channel_dm_threads_channel_identity`,
		`DROP INDEX IF EXISTS idx_channels_account_id`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqliteadapter: prepare channel table rebuild: %w", err)
		}
	}

	if receiptsExists {
		if _, err := db.ExecContext(ctx, `ALTER TABLE channel_message_receipts RENAME TO channel_message_receipts_legacy_compat_00029`); err != nil {
			return fmt.Errorf("sqliteadapter: rename channel_message_receipts: %w", err)
		}
	}
	if dmThreadsExists {
		if _, err := db.ExecContext(ctx, `ALTER TABLE channel_dm_threads RENAME TO channel_dm_threads_legacy_compat_00029`); err != nil {
			return fmt.Errorf("sqliteadapter: rename channel_dm_threads: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE channels RENAME TO channels_legacy_compat_00029`); err != nil {
		return fmt.Errorf("sqliteadapter: rename channels: %w", err)
	}

	for _, stmt := range []string{
		`CREATE TABLE channels (
			id             TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			channel_type   TEXT NOT NULL,
			persona_id     TEXT REFERENCES personas(id) ON DELETE SET NULL,
			credentials_id TEXT REFERENCES secrets(id),
			webhook_secret TEXT,
			webhook_url    TEXT,
			is_active      INTEGER NOT NULL DEFAULT 0,
			config_json    TEXT NOT NULL DEFAULT '{}',
			created_at     TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE (account_id, channel_type)
		)`,
		`INSERT INTO channels (
			id,
			account_id,
			channel_type,
			persona_id,
			credentials_id,
			webhook_secret,
			webhook_url,
			is_active,
			config_json,
			created_at,
			updated_at
		)
		SELECT
			id,
			account_id,
			channel_type,
			persona_id,
			credentials_id,
			webhook_secret,
			webhook_url,
			COALESCE(is_active, 0),
			COALESCE(config_json, '{}'),
			COALESCE(created_at, datetime('now')),
			COALESCE(updated_at, COALESCE(created_at, datetime('now')))
		FROM channels_legacy_compat_00029`,
		`CREATE INDEX idx_channels_account_id ON channels(account_id)`,
		`DROP TABLE channels_legacy_compat_00029`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqliteadapter: rebuild channels: %w", err)
		}
	}

	if dmThreadsExists {
		for _, stmt := range []string{
			`CREATE TABLE channel_dm_threads (
				id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
				channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
				channel_identity_id TEXT NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
				persona_id          TEXT NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
				thread_id           TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
				created_at          TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
				UNIQUE (channel_id, channel_identity_id, persona_id),
				UNIQUE (thread_id)
			)`,
			`INSERT INTO channel_dm_threads (
				id,
				channel_id,
				channel_identity_id,
				persona_id,
				thread_id,
				created_at,
				updated_at
			)
			SELECT
				id,
				channel_id,
				channel_identity_id,
				persona_id,
				thread_id,
				COALESCE(created_at, datetime('now')),
				COALESCE(updated_at, COALESCE(created_at, datetime('now')))
			FROM channel_dm_threads_legacy_compat_00029`,
			`CREATE INDEX idx_channel_dm_threads_channel_identity ON channel_dm_threads(channel_identity_id)`,
			`CREATE INDEX idx_channel_dm_threads_channel_id ON channel_dm_threads(channel_id)`,
			`DROP TABLE channel_dm_threads_legacy_compat_00029`,
		} {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("sqliteadapter: rebuild channel_dm_threads: %w", err)
			}
		}
	}

	if receiptsExists {
		for _, stmt := range []string{
			`CREATE TABLE channel_message_receipts (
				id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
				channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
				platform_chat_id    TEXT NOT NULL,
				platform_message_id TEXT NOT NULL,
				created_at          TEXT NOT NULL DEFAULT (datetime('now')),
				UNIQUE (channel_id, platform_chat_id, platform_message_id)
			)`,
			`INSERT INTO channel_message_receipts (
				id,
				channel_id,
				platform_chat_id,
				platform_message_id,
				created_at
			)
			SELECT
				id,
				channel_id,
				platform_chat_id,
				platform_message_id,
				COALESCE(created_at, datetime('now'))
			FROM channel_message_receipts_legacy_compat_00029`,
			`CREATE INDEX idx_channel_message_receipts_channel_id ON channel_message_receipts(channel_id)`,
			`DROP TABLE channel_message_receipts_legacy_compat_00029`,
		} {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("sqliteadapter: rebuild channel_message_receipts: %w", err)
			}
		}
	}

	return nil
}

func channelsNeedSecretsReferenceRepair(ctx context.Context, db *sql.DB) (bool, error) {
	exists, err := sqliteTableExists(ctx, db, "channels")
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	tableSQL, err := sqliteTableSQL(ctx, db, "channels")
	if err != nil {
		return false, err
	}
	normalized := strings.ToLower(strings.ReplaceAll(tableSQL, `"`, ""))
	switch {
	case strings.Contains(normalized, "references secrets_legacy_compat_00029("):
		return true, nil
	case strings.Contains(normalized, "references secrets_aligned_backup("):
		return true, nil
	case strings.Contains(normalized, "credentials_id text references secrets(id)"):
		return false, nil
	case strings.Contains(normalized, "credentials_id text"):
		return true, nil
	default:
		return false, nil
	}
}

func sqliteTableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	var count int
	if err := db.QueryRowContext(
		ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		tableName,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("sqliteadapter: query table %s: %w", tableName, err)
	}
	return count == 1, nil
}

func sqliteTableColumns(ctx context.Context, db *sql.DB, tableName string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return nil, fmt.Errorf("sqliteadapter: pragma table_info(%s): %w", tableName, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
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
			return nil, fmt.Errorf("sqliteadapter: scan table_info(%s): %w", tableName, err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqliteadapter: read table_info(%s): %w", tableName, err)
	}
	return columns, nil
}

func sqliteTableColumnDefault(ctx context.Context, db *sql.DB, tableName, columnName string) (string, bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return "", false, fmt.Errorf("sqliteadapter: pragma table_info(%s): %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return "", false, fmt.Errorf("sqliteadapter: scan table_info(%s): %w", tableName, err)
		}
		if name == columnName {
			if defaultValue.Valid {
				return defaultValue.String, true, nil
			}
			return "", false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, fmt.Errorf("sqliteadapter: read table_info(%s): %w", tableName, err)
	}
	return "", false, nil
}

func sqliteTableSQL(ctx context.Context, db *sql.DB, tableName string) (string, error) {
	var ddl sql.NullString
	if err := db.QueryRowContext(
		ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`,
		tableName,
	).Scan(&ddl); err != nil {
		return "", fmt.Errorf("sqliteadapter: query table sql %s: %w", tableName, err)
	}
	if !ddl.Valid {
		return "", nil
	}
	return ddl.String, nil
}

func sqliteRowExists(ctx context.Context, db *sql.DB, query string, args ...any) (bool, error) {
	var marker int
	err := db.QueryRowContext(ctx, query, args...).Scan(&marker)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("sqliteadapter: query row exists: %w", err)
}

func hasSQLiteColumns(columns map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if _, ok := columns[name]; !ok {
			return false
		}
	}
	return true
}

// repairScheduledTriggersSchema drops scheduled_triggers if it has the wrong schema,
// so that migration 40 can recreate it correctly.
// Runs BEFORE migrations.
func repairScheduledTriggersSchema(ctx context.Context, db *sql.DB) error {
	exists, err := sqliteTableExists(ctx, db, "scheduled_triggers")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	cols, err := sqliteTableColumns(ctx, db, "scheduled_triggers")
	if err != nil {
		return err
	}
	if hasSQLiteColumns(cols, "interval_min") {
		return nil // schema already correct
	}
	// Wrong schema from old implementation; drop so migration 40 can recreate.
	_, err = db.ExecContext(ctx, `DROP TABLE scheduled_triggers`)
	return err
}

// repairHeartbeatPersonaColumns adds heartbeat columns to personas if missing.
// Runs AFTER migrations (handles DBs that had old migration 38 applied).
func repairHeartbeatPersonaColumns(ctx context.Context, db *sql.DB) error {
	cols, err := sqliteTableColumns(ctx, db, "personas")
	if err != nil {
		return err
	}
	if !hasSQLiteColumns(cols, "heartbeat_enabled") {
		if _, err := db.ExecContext(ctx,
			`ALTER TABLE personas ADD COLUMN heartbeat_enabled INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("sqliteadapter: add heartbeat_enabled: %w", err)
		}
	}
	if !hasSQLiteColumns(cols, "heartbeat_interval_minutes") {
		if _, err := db.ExecContext(ctx,
			`ALTER TABLE personas ADD COLUMN heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30`); err != nil {
			return fmt.Errorf("sqliteadapter: add heartbeat_interval_minutes: %w", err)
		}
	}
	return nil
}
