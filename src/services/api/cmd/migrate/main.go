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

// alembicHeadRevision is the expected Alembic revision for a fully-migrated
// database. baseline refuses to proceed unless alembic_version.version_num
// matches this value, preventing silent schema drift.
const alembicHeadRevision = "0009_jobs_add_lease_token"

// baselineGooseVersion is the goose version that corresponds to the
// final Alembic head revision. Baseline seeds versions 0..baselineGooseVersion
// and never beyond, so future goose-only migrations won't be skipped.
const baselineGooseVersion int64 = 9

func cmdBaseline(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	// 1. Verify alembic_version table exists.
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

	// 2. Verify the Alembic revision matches the expected head.
	var alembicRevision string
	err = db.QueryRowContext(ctx, `SELECT version_num FROM alembic_version LIMIT 1`).Scan(&alembicRevision)
	if err != nil {
		return fmt.Errorf("read alembic_version: %w", err)
	}
	if alembicRevision != alembicHeadRevision {
		return fmt.Errorf(
			"alembic revision mismatch: got %q, expected %q; run pending alembic migrations first or use --force (not supported)",
			alembicRevision, alembicHeadRevision,
		)
	}

	// 3. Write goose_db_version in a single transaction: create table,
	//    clear any stale rows (idempotent), then seed versions 0..baselineGooseVersion.
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

	_, err = tx.ExecContext(ctx, `DELETE FROM goose_db_version`)
	if err != nil {
		return fmt.Errorf("clear goose_db_version: %w", err)
	}

	for v := int64(0); v <= baselineGooseVersion; v++ {
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
	fmt.Printf("baseline: alembic revision %q verified, marked goose versions 0..%d as applied\n",
		alembicRevision, baselineGooseVersion)
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
