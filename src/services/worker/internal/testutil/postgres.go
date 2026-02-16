package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var safeIdentifierRegex = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type PostgresDatabase struct {
	DSN      string
	Database string
}

func SetupPostgresDatabase(t *testing.T, prefix string) *PostgresDatabase {
	t.Helper()

	loadDotenvIfEnabled(t)
	baseDSN := lookupDatabaseDSN(t)
	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse database dsn failed: %v", err)
	}
	if parsed.Scheme == "postgresql+asyncpg" {
		parsed.Scheme = "postgresql"
	}

	adminURL := *parsed
	adminURL.Path = "/postgres"

	databaseName := buildDatabaseName(prefix)
	adminConn, err := pgx.Connect(context.Background(), adminURL.String())
	if err != nil {
		t.Fatalf("connect admin database failed: %v", err)
	}
	defer adminConn.Close(context.Background())

	if _, err := adminConn.Exec(context.Background(), "CREATE DATABASE "+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database failed: %v", err)
	}

	t.Cleanup(func() {
		dropTemporaryDatabase(t, adminURL.String(), databaseName)
	})

	dbURL := *parsed
	dbURL.Path = "/" + databaseName
	if err := initJobsSchema(t, dbURL.String()); err != nil {
		t.Fatalf("init jobs schema failed: %v", err)
	}
	if err := initRunsSchema(t, dbURL.String()); err != nil {
		t.Fatalf("init runs schema failed: %v", err)
	}

	return &PostgresDatabase{
		DSN:      dbURL.String(),
		Database: databaseName,
	}
}

func lookupDatabaseDSN(t *testing.T) string {
	t.Helper()

	if value, ok := lookupEnv("ARKLOOP_DATABASE_URL"); ok {
		return value
	}
	if value, ok := lookupEnv("DATABASE_URL"); ok {
		return value
	}
	t.Skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")
	return ""
}

func lookupEnv(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", false
	}
	return cleaned, true
}

func buildDatabaseName(prefix string) string {
	cleanedPrefix := strings.TrimSpace(prefix)
	if cleanedPrefix == "" {
		cleanedPrefix = "arkloop_worker_go"
	}
	cleanedPrefix = strings.ReplaceAll(cleanedPrefix, "-", "_")
	cleanedPrefix = strings.ReplaceAll(cleanedPrefix, ".", "_")
	return fmt.Sprintf("%s_%s", cleanedPrefix, strings.ReplaceAll(uuid.NewString(), "-", ""))
}

func quoteIdentifier(name string) string {
	if !safeIdentifierRegex.MatchString(name) {
		panic("illegal identifier")
	}
	return `"` + name + `"`
}

func initJobsSchema(t *testing.T, dsn string) error {
	t.Helper()

	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE jobs (
			id UUID PRIMARY KEY,
			job_type TEXT NOT NULL,
			payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			status TEXT NOT NULL DEFAULT 'queued',
			available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			leased_until TIMESTAMPTZ NULL,
			lease_token UUID NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX ix_jobs_job_type ON jobs (job_type)`,
		`CREATE INDEX ix_jobs_status_available_at ON jobs (status, available_at)`,
		`CREATE INDEX ix_jobs_status_leased_until ON jobs (status, leased_until)`,
	}

	for _, statement := range statements {
		if _, err := conn.Exec(context.Background(), statement); err != nil {
			return err
		}
	}
	return nil
}

func initRunsSchema(t *testing.T, dsn string) error {
	t.Helper()

	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	statements := []string{
		`CREATE TABLE runs (
			id UUID PRIMARY KEY,
			org_id UUID NOT NULL,
			thread_id UUID NOT NULL,
			created_by_user_id UUID NULL,
			status TEXT NOT NULL DEFAULT 'running',
			next_event_seq BIGINT NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE run_events (
			event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			run_id UUID NOT NULL,
			seq BIGINT NOT NULL,
			ts TIMESTAMPTZ NOT NULL DEFAULT now(),
			type TEXT NOT NULL,
			data_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			tool_name TEXT NULL,
			error_class TEXT NULL,
			CONSTRAINT uq_run_events_run_id_seq UNIQUE (run_id, seq)
		)`,
		`CREATE TABLE messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id UUID NOT NULL,
			thread_id UUID NOT NULL,
			created_by_user_id UUID NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}

	for _, statement := range statements {
		if _, err := conn.Exec(context.Background(), statement); err != nil {
			return err
		}
	}
	return nil
}

func dropTemporaryDatabase(t *testing.T, adminDSN string, databaseName string) {
	t.Helper()

	conn, err := pgx.Connect(context.Background(), adminDSN)
	if err != nil {
		t.Fatalf("connect admin database for cleanup failed: %v", err)
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(
		context.Background(),
		`SELECT pg_terminate_backend(pid)
		 FROM pg_stat_activity
		 WHERE datname = $1
		   AND pid <> pg_backend_pid()`,
		databaseName,
	); err != nil {
		t.Fatalf("terminate backend failed: %v", err)
	}

	if _, err := conn.Exec(context.Background(), "DROP DATABASE "+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("drop database failed: %v", err)
	}
}

func loadDotenvIfEnabled(t *testing.T) {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv("ARKLOOP_LOAD_DOTENV"))
	if raw == "" {
		return
	}
	lower := strings.ToLower(raw)
	if lower != "1" && lower != "true" && lower != "yes" && lower != "on" {
		return
	}

	dotenvPath := filepath.Join(repoRoot(t), ".env")
	content, err := os.ReadFile(dotenvPath)
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, strings.TrimSpace(value))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	current := cwd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(current, "pyproject.toml")); err == nil {
			return current
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}
	return cwd
}
