package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedFS embed.FS

const ExpectedVersion int64 = 12

func migrationsFS() fs.FS {
	sub, err := fs.Sub(embedFS, "migrations")
	if err != nil {
		panic(fmt.Sprintf("migrate: embedded sub-fs: %v", err))
	}
	return sub
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
	defer db.Close()

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
	defer db.Close()

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

func CurrentVersion(ctx context.Context, dsn string) (int64, error) {
	db, err := openDB(dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()

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
