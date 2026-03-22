//go:build desktop

package sqliteadapter

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedFS embed.FS

// ExpectedVersion is the highest migration version embedded in this package.
var ExpectedVersion int64 = expectedVersionFromEmbeddedMigrations()

// EmbeddedMigrationCount is the number of embedded migration files.
var EmbeddedMigrationCount int = embeddedMigrationCount()

func migrationsFS() fs.FS {
	sub, err := fs.Sub(embedFS, "migrations")
	if err != nil {
		panic(fmt.Sprintf("sqliteadapter: embedded sub-fs: %v", err))
	}
	return sub
}

func expectedVersionFromEmbeddedMigrations() int64 {
	entries, err := fs.ReadDir(migrationsFS(), ".")
	if err != nil {
		panic(fmt.Sprintf("sqliteadapter: read embedded migrations: %v", err))
	}

	var max int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		base := strings.TrimSuffix(name, ".sql")
		prefix, _, _ := strings.Cut(base, "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			panic(fmt.Sprintf("sqliteadapter: invalid migration filename %q", name))
		}
		if version > max {
			max = version
		}
	}

	if max <= 0 {
		panic("sqliteadapter: embedded migrations empty")
	}
	return max
}

func embeddedMigrationCount() int {
	entries, err := fs.ReadDir(migrationsFS(), ".")
	if err != nil {
		panic(fmt.Sprintf("sqliteadapter: read embedded migrations: %v", err))
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".sql") {
			count++
		}
	}
	if count == 0 {
		panic("sqliteadapter: embedded migrations empty")
	}
	return count
}

func newProvider(db *sql.DB) (*goose.Provider, error) {
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		migrationsFS(),
	)
	if err != nil {
		return nil, fmt.Errorf("sqliteadapter: new provider: %w", err)
	}
	return provider, nil
}

// Up applies all pending migrations.
func Up(ctx context.Context, db *sql.DB) ([]*goose.MigrationResult, error) {
	provider, err := newProvider(db)
	if err != nil {
		return nil, err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqliteadapter: up: %w", err)
	}
	return results, nil
}

// DownOne rolls back the most recent migration.
func DownOne(ctx context.Context, db *sql.DB) (*goose.MigrationResult, error) {
	provider, err := newProvider(db)
	if err != nil {
		return nil, err
	}
	result, err := provider.Down(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqliteadapter: down: %w", err)
	}
	return result, nil
}

// DownAll rolls back all migrations.
func DownAll(ctx context.Context, db *sql.DB) (int, error) {
	provider, err := newProvider(db)
	if err != nil {
		return 0, err
	}

	count := 0
	for {
		result, err := provider.Down(ctx)
		if err != nil {
			if errors.Is(err, goose.ErrNoNextVersion) {
				break
			}
			return count, fmt.Errorf("sqliteadapter: down all at step %d: %w", count+1, err)
		}
		if result == nil {
			break
		}
		count++
	}
	return count, nil
}

// CurrentVersion returns the current schema version.
func CurrentVersion(ctx context.Context, db *sql.DB) (int64, error) {
	provider, err := newProvider(db)
	if err != nil {
		return 0, err
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("sqliteadapter: get version: %w", err)
	}
	return version, nil
}

// AutoMigrate opens a SQLite database, applies pending migrations, and returns
// a ready Pool. This is the primary entry point for Desktop mode.
func AutoMigrate(ctx context.Context, dataSourceName string) (*Pool, error) {
	pool, err := Open(dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("sqliteadapter: open: %w", err)
	}
	// Pre-migration repair: fix any schema issues that would block migrations.
	if err := repairSchemasPreMigration(ctx, pool.Unwrap()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("sqliteadapter: pre-migrate repair: %w", err)
	}
	_, err = Up(ctx, pool.Unwrap())
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("sqliteadapter: auto-migrate: %w", err)
	}
	if err := repairLegacySchemas(ctx, pool.Unwrap()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("sqliteadapter: repair legacy schemas: %w", err)
	}
	return pool, nil
}
