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
var dotenvKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type PostgresDatabase struct {
	DSN      string
	Database string
}

func SetupPostgresDatabase(t *testing.T, prefix string) *PostgresDatabase {
	t.Helper()

	requireIntegrationTests(t)
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
	t.Skip("ARKLOOP_DATABASE_URL (or compatible DATABASE_URL) not set")
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

func requireIntegrationTests(t *testing.T) {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv("ARKLOOP_RUN_INTEGRATION_TESTS"))
	if raw == "" {
		t.Skip("integration tests disabled")
	}
	lower := strings.ToLower(raw)
	if lower == "1" || lower == "true" || lower == "yes" || lower == "on" {
		return
	}
	t.Skip("integration tests disabled")
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
			worker_tags TEXT[] NOT NULL DEFAULT '{}',
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
		`CREATE SEQUENCE run_events_seq_global START 1`,
		`CREATE TABLE platform_settings (
			key        TEXT        PRIMARY KEY,
			value      TEXT        NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE org_settings (
			org_id     UUID        NOT NULL,
			key        TEXT        NOT NULL,
			value      TEXT        NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (org_id, key)
		)`,
		`CREATE INDEX ix_org_settings_key ON org_settings (key)`,
		`CREATE TABLE personas (
			id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id               UUID        NULL,
			persona_key          TEXT        NOT NULL,
			version              TEXT        NOT NULL,
			display_name         TEXT        NOT NULL,
			description          TEXT        NULL,
			prompt_md            TEXT        NOT NULL,
			tool_allowlist       TEXT[]      NOT NULL DEFAULT '{}',
			tool_denylist        TEXT[]      NOT NULL DEFAULT '{}',
			budgets_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
			model                TEXT        NULL,
			reasoning_mode       TEXT        NOT NULL DEFAULT 'auto',
			prompt_cache_control TEXT        NOT NULL DEFAULT 'none',
			executor_type        TEXT        NOT NULL DEFAULT 'agent.simple',
			executor_config_json JSONB       NOT NULL DEFAULT '{}'::jsonb,
			preferred_credential TEXT        NULL,
			is_active            BOOLEAN     NOT NULL DEFAULT TRUE,
			created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT uq_personas_org_key_version UNIQUE NULLS NOT DISTINCT (org_id, persona_key, version)
		)`,
		`CREATE TABLE secrets (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id          UUID        NULL,
			scope           TEXT        NOT NULL DEFAULT 'org',
			encrypted_value TEXT        NOT NULL,
			key_version     INT         NOT NULL DEFAULT 1,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE llm_credentials (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id          UUID        NOT NULL,
			provider        TEXT        NOT NULL,
			name            TEXT        NOT NULL,
			secret_id       UUID        NULL,
			base_url        TEXT        NULL,
			openai_api_mode TEXT        NULL,
			advanced_json   JSONB       NOT NULL DEFAULT '{}'::jsonb,
			revoked_at      TIMESTAMPTZ NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE llm_routes (
			id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id              UUID        NOT NULL,
			credential_id       UUID        NOT NULL,
			model               TEXT        NOT NULL,
			priority            INT         NOT NULL DEFAULT 0,
			is_default          BOOLEAN     NOT NULL DEFAULT false,
			tags                TEXT[]      NOT NULL DEFAULT '{}',
			when_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			multiplier          DOUBLE PRECISION NOT NULL DEFAULT 1.0,
			cost_per_1k_input      DOUBLE PRECISION NULL,
			cost_per_1k_output     DOUBLE PRECISION NULL,
			cost_per_1k_cache_write DOUBLE PRECISION NULL,
			cost_per_1k_cache_read  DOUBLE PRECISION NULL,
			created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX ux_llm_routes_credential_model_lower ON llm_routes (credential_id, lower(model))`,
		`CREATE UNIQUE INDEX ux_llm_routes_credential_default ON llm_routes (credential_id) WHERE is_default = TRUE`,
		`CREATE TABLE threads (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id             UUID        NOT NULL,
			created_by_user_id UUID        NULL,
			project_id         UUID        NULL,
			is_private         BOOLEAN     NOT NULL DEFAULT FALSE,
			expires_at         TIMESTAMPTZ NULL,
			deleted_at         TIMESTAMPTZ NULL,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE runs (
			id                  UUID        PRIMARY KEY,
			org_id              UUID        NOT NULL,
			thread_id           UUID        NOT NULL,
			profile_ref         TEXT        NULL,
			workspace_ref       TEXT        NULL,
			parent_run_id       UUID        NULL,
			created_by_user_id  UUID        NULL,
			status              TEXT        NOT NULL DEFAULT 'running',
			status_updated_at   TIMESTAMPTZ NULL,
			completed_at        TIMESTAMPTZ NULL,
			failed_at           TIMESTAMPTZ NULL,
			duration_ms         BIGINT      NULL,
			total_input_tokens  BIGINT      NULL,
			total_output_tokens BIGINT      NULL,
			total_cost_usd      NUMERIC     NULL,
			model               TEXT        NULL,
			persona_id            TEXT        NULL,
			deleted_at          TIMESTAMPTZ NULL,
			created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE default_workspace_bindings (
			profile_ref       TEXT        NOT NULL,
			owner_user_id     UUID        NULL,
			org_id            UUID        NOT NULL,
			binding_scope     TEXT        NOT NULL,
			binding_target_id UUID        NOT NULL,
			workspace_ref     TEXT        NOT NULL,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (org_id, profile_ref, binding_scope, binding_target_id)
		)`,
		`CREATE UNIQUE INDEX idx_default_workspace_bindings_workspace_ref
		    ON default_workspace_bindings (workspace_ref)`,
		`CREATE TABLE shell_sessions (
			session_ref           TEXT        PRIMARY KEY,
			org_id                UUID        NOT NULL,
			profile_ref           TEXT        NOT NULL,
			workspace_ref         TEXT        NOT NULL,
			project_id            UUID        NULL,
			thread_id             UUID        NULL,
			run_id                UUID        NULL,
			share_scope           TEXT        NOT NULL,
			state                 TEXT        NOT NULL,
			live_session_id       TEXT        NULL,
			latest_restore_rev    TEXT        NULL,
			default_binding_key   TEXT        NULL,
			last_used_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
			metadata_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX idx_shell_sessions_org_thread ON shell_sessions (org_id, thread_id)`,
		`CREATE INDEX idx_shell_sessions_org_workspace ON shell_sessions (org_id, workspace_ref)`,
		`CREATE INDEX idx_shell_sessions_org_run ON shell_sessions (org_id, run_id)`,
		`CREATE INDEX idx_shell_sessions_org_profile_default_binding_updated
		    ON shell_sessions (org_id, profile_ref, default_binding_key, updated_at DESC)
		    WHERE default_binding_key IS NOT NULL`,
		`CREATE TABLE profile_registries (
			profile_ref             TEXT        PRIMARY KEY,
			org_id                  UUID        NOT NULL,
			owner_user_id           UUID        NULL,
			latest_manifest_rev     TEXT        NULL,
			lease_holder_id         TEXT        NULL,
			lease_until             TIMESTAMPTZ NULL,
			default_workspace_ref   TEXT        NULL,
			store_key               TEXT        NULL,
			flush_state             TEXT        NOT NULL DEFAULT 'idle',
			flush_retry_count       INTEGER     NOT NULL DEFAULT 0,
			last_used_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
			last_flush_failed_at    TIMESTAMPTZ NULL,
			last_flush_succeeded_at TIMESTAMPTZ NULL,
			metadata_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT profile_registries_lease_consistency CHECK (
				(lease_holder_id IS NULL AND lease_until IS NULL)
				OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
			)
		)`,
		`CREATE INDEX idx_profile_registries_org_id ON profile_registries (org_id)`,
		`CREATE TABLE workspace_registries (
			workspace_ref             TEXT        PRIMARY KEY,
			org_id                    UUID        NOT NULL,
			owner_user_id             UUID        NULL,
			project_id                UUID        NULL,
			latest_manifest_rev       TEXT        NULL,
			lease_holder_id           TEXT        NULL,
			lease_until               TIMESTAMPTZ NULL,
			default_shell_session_ref TEXT        NULL,
			store_key                 TEXT        NULL,
			flush_state               TEXT        NOT NULL DEFAULT 'idle',
			flush_retry_count         INTEGER     NOT NULL DEFAULT 0,
			last_used_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
			last_flush_failed_at      TIMESTAMPTZ NULL,
			last_flush_succeeded_at   TIMESTAMPTZ NULL,
			metadata_json             JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT workspace_registries_lease_consistency CHECK (
				(lease_holder_id IS NULL AND lease_until IS NULL)
				OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
			)
		)`,
		`CREATE INDEX idx_workspace_registries_org_id ON workspace_registries (org_id)`,
		`CREATE TABLE run_events (
			event_id    UUID        NOT NULL DEFAULT gen_random_uuid(),
			run_id      UUID        NOT NULL,
			seq         BIGINT      NOT NULL DEFAULT nextval('run_events_seq_global'),
			ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
			type        TEXT        NOT NULL,
			data_json   JSONB       NOT NULL DEFAULT '{}'::jsonb,
			tool_name   TEXT        NULL,
			error_class TEXT        NULL,
			CONSTRAINT pk_run_events PRIMARY KEY (event_id, ts),
			CONSTRAINT uq_run_events_run_id_seq UNIQUE (run_id, seq, ts)
		)`,
		`CREATE TABLE messages (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id             UUID        NOT NULL,
			thread_id          UUID        NOT NULL,
			created_by_user_id UUID        NULL,
			role               TEXT        NOT NULL,
			content            TEXT        NOT NULL,
			content_json       JSONB       NULL,
			hidden             BOOLEAN     NOT NULL DEFAULT FALSE,
			deleted_at         TIMESTAMPTZ NULL,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE user_memory_snapshots (
			org_id       UUID        NOT NULL,
			user_id      UUID        NOT NULL,
			agent_id     TEXT        NOT NULL DEFAULT 'default',
			memory_block TEXT        NOT NULL,
			hits_json    JSONB       NULL,
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (org_id, user_id, agent_id)
		)`,
		`CREATE TABLE usage_records (
			id                    UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id                UUID           NOT NULL,
			run_id                UUID           NOT NULL,
			model                 TEXT           NOT NULL DEFAULT '',
			input_tokens          BIGINT         NOT NULL DEFAULT 0,
			output_tokens         BIGINT         NOT NULL DEFAULT 0,
			cache_creation_tokens BIGINT         NOT NULL DEFAULT 0,
			cache_read_tokens     BIGINT         NOT NULL DEFAULT 0,
			cached_tokens         BIGINT         NOT NULL DEFAULT 0,
			cost_usd              NUMERIC(18, 8) NOT NULL DEFAULT 0,
			usage_type            TEXT           NOT NULL DEFAULT 'llm',
			recorded_at           TIMESTAMPTZ    NOT NULL DEFAULT now(),
			CONSTRAINT uq_usage_records_run_id_usage_type UNIQUE (run_id, usage_type)
		)`,
		`CREATE INDEX idx_usage_records_org_recorded ON usage_records (org_id, recorded_at)`,
		`CREATE TABLE credits (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id     UUID        NOT NULL UNIQUE,
			balance    BIGINT      NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE credit_transactions (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id         UUID        NOT NULL,
			amount         BIGINT      NOT NULL,
			type           TEXT        NOT NULL,
			reference_type TEXT        NULL,
			reference_id   UUID        NULL,
			note           TEXT        NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
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

	allowedKeys := map[string]struct{}{
		"DATABASE_URL":         {},
		"ARKLOOP_DATABASE_URL": {},
	}

	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := parseDotenvLine(line)
		if !ok {
			continue
		}
		if _, allowed := allowedKeys[key]; !allowed {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, value)
	}
}

func parseDotenvLine(raw string) (string, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}

	idx := strings.Index(line, "=")
	if idx <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:idx])
	if key == "" || !dotenvKeyRegex.MatchString(key) {
		return "", "", false
	}

	value := strings.TrimSpace(line[idx+1:])
	if value == "" {
		return key, "", true
	}

	if len(value) >= 2 {
		quote := value[0]
		if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
			return key, value[1 : len(value)-1], true
		}
	}

	return key, stripInlineComment(value), true
}

func stripInlineComment(value string) string {
	// only handles "value  # comment" pattern, preserves "#" without preceding space.
	for i := 1; i < len(value); i++ {
		if value[i] != '#' {
			continue
		}
		prev := value[i-1]
		if prev != ' ' && prev != '\t' {
			continue
		}
		start := i - 1
		for start > 0 {
			ch := value[start-1]
			if ch != ' ' && ch != '\t' {
				break
			}
			start--
		}
		return strings.TrimSpace(value[:start])
	}
	return strings.TrimSpace(value)
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
