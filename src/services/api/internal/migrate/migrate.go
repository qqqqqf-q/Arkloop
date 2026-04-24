package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedFS embed.FS

var ExpectedVersion int64 = expectedVersionFromEmbeddedMigrations()
var EmbeddedMigrationCount int = embeddedMigrationCount()

func migrationsFS() fs.FS {
	sub, err := fs.Sub(embedFS, "migrations")
	if err != nil {
		panic(fmt.Sprintf("migrate: embedded sub-fs: %v", err))
	}
	return sub
}

func expectedVersionFromEmbeddedMigrations() int64 {
	entries, err := fs.ReadDir(migrationsFS(), ".")
	if err != nil {
		panic(fmt.Sprintf("migrate: read embedded migrations: %v", err))
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
			panic(fmt.Sprintf("migrate: invalid migration filename %q", name))
		}
		if version > max {
			max = version
		}
	}

	if max <= 0 {
		panic("migrate: embedded migrations empty")
	}
	return max
}

func embeddedMigrationCount() int {
	entries, err := fs.ReadDir(migrationsFS(), ".")
	if err != nil {
		panic(fmt.Sprintf("migrate: read embedded migrations: %v", err))
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
		panic("migrate: embedded migrations empty")
	}
	return count
}

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("migrate: open db: %w", err)
	}
	return db, nil
}

func newProvider(db *sql.DB) (*goose.Provider, error) {
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrationsFS(),
	)
	if err != nil {
		return nil, fmt.Errorf("migrate: new provider: %w", err)
	}
	return provider, nil
}

func Up(ctx context.Context, dsn string) ([]*goose.MigrationResult, error) {
	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	provider, err := newProvider(db)
	if err != nil {
		return nil, err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: up: %w", err)
	}
	return results, nil
}

func DownOne(ctx context.Context, dsn string) (*goose.MigrationResult, error) {
	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	provider, err := newProvider(db)
	if err != nil {
		return nil, err
	}
	result, err := provider.Down(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: down: %w", err)
	}
	return result, nil
}

func DownAll(ctx context.Context, dsn string) (int, error) {
	db, err := openDB(dsn)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()

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
			return count, fmt.Errorf("migrate: down all at step %d: %w", count+1, err)
		}
		if result == nil {
			break
		}
		count++
	}
	return count, nil
}

func CurrentVersion(ctx context.Context, dsn string) (int64, error) {
	db, err := openDB(dsn)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()

	provider, err := newProvider(db)
	if err != nil {
		return 0, err
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate: get version: %w", err)
	}
	return version, nil
}

func CheckVersion(ctx context.Context, dsn string) (current int64, expected int64, match bool, err error) {
	current, err = CurrentVersion(ctx, dsn)
	if err != nil {
		return 0, ExpectedVersion, false, err
	}
	return current, ExpectedVersion, current == ExpectedVersion, nil
}
