package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
)

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
