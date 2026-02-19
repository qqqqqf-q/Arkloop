package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"arkloop/services/api/internal/app"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}

	dsn := resolveDSN()
	if dsn == "" {
		return fmt.Errorf("ARKLOOP_DATABASE_URL (or DATABASE_URL) is required")
	}
	dsn = data.NormalizePostgresDSN(dsn)

	cmd := "up"
	if len(os.Args) > 1 {
		cmd = strings.TrimSpace(os.Args[1])
	}

	ctx := context.Background()

	switch cmd {
	case "up":
		return cmdUp(ctx, dsn)
	case "down":
		return cmdDown(ctx, dsn)
	case "status":
		return cmdStatus(ctx, dsn)
	case "baseline":
		return cmdBaseline(ctx, dsn)
	default:
		return fmt.Errorf("unknown command: %s (available: up, down, status, baseline)", cmd)
	}
}

func cmdUp(ctx context.Context, dsn string) error {
	results, err := migrate.Up(ctx, dsn)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("no pending migrations")
		return nil
	}
	for _, r := range results {
		fmt.Printf("applied %05d %s\n", r.Source.Version, r.Source.Path)
	}
	fmt.Printf("done: %d migration(s) applied\n", len(results))
	return nil
}

func cmdDown(ctx context.Context, dsn string) error {
	result, err := migrate.DownOne(ctx, dsn)
	if err != nil {
		return err
	}
	if result == nil {
		fmt.Println("no migrations to roll back")
		return nil
	}
	fmt.Printf("rolled back %05d %s\n", result.Source.Version, result.Source.Path)
	return nil
}

func cmdStatus(ctx context.Context, dsn string) error {
	current, expected, match, err := migrate.CheckVersion(ctx, dsn)
	if err != nil {
		return err
	}
	fmt.Printf("current:  %d\n", current)
	fmt.Printf("expected: %d\n", expected)
	if match {
		fmt.Println("status:   ok")
	} else {
		fmt.Println("status:   mismatch")
	}
	return nil
}

func cmdBaseline(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var hasAlembic bool
	err = db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'alembic_version'
		)`,
	).Scan(&hasAlembic)
	if err != nil {
		return fmt.Errorf("check alembic_version: %w", err)
	}
	if !hasAlembic {
		return fmt.Errorf("alembic_version table not found; baseline only applies to existing databases migrated by alembic")
	}

	// run goose up to create goose_db_version table and apply all migrations
	// that already exist in the database (goose skips CREATE TABLE IF EXISTS via provider)
	// Instead, we use goose provider to mark versions without running SQL
	_, err = migrate.Up(ctx, dsn)
	if err != nil {
		// if tables already exist, the migration will fail
		// fall back to manual version seeding
		return baselineManual(ctx, db)
	}
	fmt.Println("baseline: migrations applied successfully")
	return nil
}

func baselineManual(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS goose_db_version (
			id SERIAL PRIMARY KEY,
			version_id BIGINT NOT NULL,
			is_applied BOOLEAN NOT NULL,
			tstamp TIMESTAMP DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create goose_db_version: %w", err)
	}

	var count int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = $1`, migrate.ExpectedVersion).Scan(&count)
	if err != nil {
		return fmt.Errorf("check existing baseline: %w", err)
	}
	if count > 0 {
		fmt.Println("baseline: already applied")
		return nil
	}

	for v := int64(0); v <= migrate.ExpectedVersion; v++ {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`,
			v,
		)
		if err != nil {
			return fmt.Errorf("insert version %d: %w", v, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit baseline: %w", err)
	}
	fmt.Printf("baseline: marked versions 0..%d as applied\n", migrate.ExpectedVersion)
	return nil
}

func resolveDSN() string {
	for _, key := range []string{"ARKLOOP_DATABASE_URL", "DATABASE_URL"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}
