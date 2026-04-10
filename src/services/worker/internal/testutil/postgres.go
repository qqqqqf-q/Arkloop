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

	sharedtestutil "arkloop/services/shared/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var dotenvKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type PostgresDatabase struct {
	DSN      string
	Database string
}

func SetupPostgresDatabase(t *testing.T, prefix string) *PostgresDatabase {
	t.Helper()

	sharedtestutil.RequireIntegrationTests(t)
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

	if _, err := adminConn.Exec(context.Background(), "CREATE DATABASE "+sharedtestutil.QuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database failed: %v", err)
	}

	t.Cleanup(func() {
		sharedtestutil.DropTemporaryDatabase(t, adminURL.String(), databaseName)
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

func buildDatabaseName(prefix string) string {
	cleanedPrefix := strings.TrimSpace(prefix)
	if cleanedPrefix == "" {
		cleanedPrefix = "arkloop_worker_go"
	}
	cleanedPrefix = strings.ReplaceAll(cleanedPrefix, "-", "_")
	cleanedPrefix = strings.ReplaceAll(cleanedPrefix, ".", "_")
	return fmt.Sprintf("%s_%s", cleanedPrefix, strings.ReplaceAll(uuid.NewString(), "-", ""))
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
		`CREATE UNIQUE INDEX ux_jobs_run_execute_active_run ON jobs ((payload_json->>'run_id')) WHERE job_type = 'run.execute' AND status IN ('queued', 'leased')`,
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
		`CREATE TABLE account_settings (
			account_id     UUID        NOT NULL,
			key        TEXT        NOT NULL,
			value      TEXT        NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (account_id, key)
		)`,
		`CREATE INDEX ix_account_settings_key ON account_settings (key)`,
		`CREATE TABLE personas (
			id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id               UUID        NULL,
			project_id           UUID        NULL,
			persona_key          TEXT        NOT NULL,
			version              TEXT        NOT NULL,
			display_name         TEXT        NOT NULL,
			description          TEXT        NULL,
			soul_md              TEXT        NOT NULL DEFAULT '',
			user_selectable      BOOLEAN     NOT NULL DEFAULT FALSE,
			selector_name        TEXT        NULL,
			selector_order       INTEGER     NULL,
			prompt_md            TEXT        NOT NULL,
			tool_allowlist       TEXT[]      NOT NULL DEFAULT '{}',
			tool_denylist        TEXT[]      NOT NULL DEFAULT '{}',
			budgets_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
			roles_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			title_summarize_json JSONB       NULL,
			result_summarize_json JSONB      NULL,
			conditional_tools_json JSONB     NULL,
			model                TEXT        NULL,
			reasoning_mode       TEXT        NOT NULL DEFAULT 'auto',
			stream_thinking      BOOLEAN     NOT NULL DEFAULT TRUE,
			prompt_cache_control TEXT        NOT NULL DEFAULT 'none',
			executor_type        TEXT        NOT NULL DEFAULT 'agent.simple',
			executor_config_json JSONB       NOT NULL DEFAULT '{}'::jsonb,
			preferred_credential TEXT        NULL,
			is_active            BOOLEAN     NOT NULL DEFAULT TRUE,
			created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
			sync_mode            TEXT        NOT NULL DEFAULT 'none',
			mirrored_file_dir    TEXT        NULL,
			last_synced_at       TIMESTAMPTZ NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_personas_project_key_version ON personas (project_id, persona_key, version) WHERE project_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_personas_platform_key_version ON personas (persona_key, version) WHERE project_id IS NULL`,
		`CREATE TABLE secrets (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id          UUID        NULL,
			owner_kind      TEXT        NOT NULL DEFAULT 'platform',
			encrypted_value TEXT        NOT NULL,
			key_version     INT         NOT NULL DEFAULT 1,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE llm_credentials (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id          UUID        NULL,
			owner_kind      TEXT        NOT NULL DEFAULT 'platform',
			provider        TEXT        NOT NULL,
			name            TEXT        NOT NULL,
			secret_id       UUID        NULL,
			key_prefix      TEXT        NULL,
			base_url        TEXT        NULL,
			openai_api_mode TEXT        NULL,
			advanced_json   JSONB       NOT NULL DEFAULT '{}'::jsonb,
			revoked_at      TIMESTAMPTZ NULL,
			last_used_at    TIMESTAMPTZ NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX llm_credentials_user_name_idx ON llm_credentials (account_id, name) WHERE owner_kind = 'user'`,
		`CREATE UNIQUE INDEX llm_credentials_platform_name_idx ON llm_credentials (name) WHERE owner_kind = 'platform'`,
		`CREATE TABLE llm_routes (
			id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id              UUID        NULL,
			credential_id       UUID        NOT NULL,
			model               TEXT        NOT NULL,
			priority            INT         NOT NULL DEFAULT 0,
			is_default          BOOLEAN     NOT NULL DEFAULT false,
			tags                TEXT[]      NOT NULL DEFAULT '{}',
			when_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			advanced_json       JSONB       NOT NULL DEFAULT '{}'::jsonb,
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
			account_id             UUID        NOT NULL,
			created_by_user_id UUID        NULL,
			project_id         UUID        NULL,
			is_private         BOOLEAN     NOT NULL DEFAULT FALSE,
			expires_at         TIMESTAMPTZ NULL,
			deleted_at         TIMESTAMPTZ NULL,
			next_message_seq   BIGINT      NOT NULL DEFAULT 1,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE account_memberships (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id     UUID        NOT NULL,
			user_id    UUID        NOT NULL,
			role       TEXT        NOT NULL,
			role_id    UUID        NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT uq_account_memberships_account_id_user_id UNIQUE (account_id, user_id)
		)`,
		`CREATE TABLE runs (
			id                  UUID        PRIMARY KEY,
			account_id              UUID        NOT NULL,
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
		`CREATE TABLE sub_agents (
			id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id                UUID        NOT NULL,
			parent_run_id         UUID        NOT NULL,
			parent_thread_id      UUID        NOT NULL,
			root_run_id           UUID        NOT NULL,
			root_thread_id        UUID        NOT NULL,
			depth                 INTEGER     NOT NULL,
			role                  TEXT        NULL,
			persona_id            TEXT        NULL,
			nickname              TEXT        NULL,
			source_type           TEXT        NOT NULL,
			context_mode          TEXT        NOT NULL,
			status                TEXT        NOT NULL,
			current_run_id        UUID        NULL,
			last_completed_run_id UUID        NULL,
			last_output_ref       TEXT        NULL,
			last_error            TEXT        NULL,
			created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
			started_at            TIMESTAMPTZ NULL,
			completed_at          TIMESTAMPTZ NULL,
			closed_at             TIMESTAMPTZ NULL
		)`,
		`CREATE INDEX idx_sub_agents_account_id ON sub_agents (account_id)`,
		`CREATE INDEX idx_sub_agents_parent_run_id ON sub_agents (parent_run_id)`,
		`CREATE INDEX idx_sub_agents_root_run_id ON sub_agents (root_run_id)`,
		`CREATE INDEX idx_sub_agents_current_run_id ON sub_agents (current_run_id)`,
		`CREATE INDEX idx_sub_agents_status ON sub_agents (status)`,
		`CREATE TABLE sub_agent_events (
			event_id      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			sub_agent_id  UUID        NOT NULL,
			run_id        UUID        NULL,
			seq           BIGINT      NOT NULL DEFAULT nextval('run_events_seq_global'),
			ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
			type          TEXT        NOT NULL,
			data_json     JSONB       NOT NULL DEFAULT '{}'::jsonb,
			error_class   TEXT        NULL,
			CONSTRAINT uq_sub_agent_events_sub_agent_id_seq UNIQUE (sub_agent_id, seq)
		)`,
		`CREATE INDEX idx_sub_agent_events_sub_agent_id_ts ON sub_agent_events (sub_agent_id, ts)`,
		`CREATE INDEX idx_sub_agent_events_type ON sub_agent_events (type)`,
		`CREATE INDEX idx_sub_agent_events_run_id ON sub_agent_events (run_id)`,
		`CREATE TABLE sub_agent_pending_inputs (
			id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			sub_agent_id  UUID        NOT NULL,
			seq           BIGINT      NOT NULL DEFAULT nextval('run_events_seq_global'),
			input         TEXT        NOT NULL,
			priority      BOOLEAN     NOT NULL DEFAULT FALSE,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT uq_sub_agent_pending_inputs_sub_agent_id_seq UNIQUE (sub_agent_id, seq)
		)`,
		`CREATE INDEX idx_sub_agent_pending_inputs_sub_agent_id_seq ON sub_agent_pending_inputs (sub_agent_id, priority DESC, seq ASC)`,
		`CREATE TABLE sub_agent_context_snapshots (
			sub_agent_id  UUID        PRIMARY KEY REFERENCES sub_agents(id) ON DELETE CASCADE,
			snapshot_json JSONB       NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX idx_sub_agent_context_snapshots_updated_at ON sub_agent_context_snapshots (updated_at)`,
		`CREATE TABLE thread_compaction_snapshots (
			id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id             UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			thread_id              UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
			summary_text           TEXT        NOT NULL,
			metadata_json          JSONB       NOT NULL DEFAULT '{}'::jsonb,
			supersedes_snapshot_id UUID        NULL REFERENCES thread_compaction_snapshots(id) ON DELETE SET NULL,
			is_active              BOOLEAN     NOT NULL DEFAULT TRUE,
			created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX uq_thread_compaction_snapshots_active_thread
		    ON thread_compaction_snapshots (thread_id)
		 WHERE is_active = TRUE`,
		`CREATE INDEX ix_thread_compaction_snapshots_thread_created_at
		    ON thread_compaction_snapshots (thread_id, created_at DESC)`,
		`CREATE TABLE thread_context_replacements (
			id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id       UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			thread_id        UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
			start_thread_seq BIGINT      NOT NULL,
			end_thread_seq   BIGINT      NOT NULL,
			summary_text     TEXT        NOT NULL,
			layer            INTEGER     NOT NULL DEFAULT 1,
			metadata_json    JSONB       NOT NULL DEFAULT '{}'::jsonb,
			superseded_at    TIMESTAMPTZ NULL,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT chk_thread_context_replacements_range CHECK (start_thread_seq <= end_thread_seq)
		)`,
		`CREATE INDEX idx_thread_context_replacements_thread_active
		    ON thread_context_replacements (thread_id, start_thread_seq, end_thread_seq, layer DESC, created_at DESC)
		 WHERE superseded_at IS NULL`,
		`CREATE INDEX idx_thread_context_replacements_thread_created
		    ON thread_context_replacements (thread_id, created_at DESC)`,
		`CREATE TABLE default_workspace_bindings (
			profile_ref       TEXT        NOT NULL,
			owner_user_id     UUID        NULL,
			account_id            UUID        NOT NULL,
			binding_scope     TEXT        NOT NULL,
			binding_target_id UUID        NOT NULL,
			workspace_ref     TEXT        NOT NULL,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (account_id, profile_ref, binding_scope, binding_target_id)
		)`,
		`CREATE UNIQUE INDEX idx_default_workspace_bindings_workspace_ref
		    ON default_workspace_bindings (workspace_ref)`,
		`CREATE TABLE shell_sessions (
			session_ref           TEXT        PRIMARY KEY,
			session_type          TEXT        NOT NULL DEFAULT 'shell',
			account_id                UUID        NOT NULL,
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
			lease_owner_id        TEXT        NULL,
			lease_until           TIMESTAMPTZ NULL,
			lease_epoch           BIGINT      NOT NULL DEFAULT 0,
			last_used_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
			metadata_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT shell_sessions_session_type_check CHECK (session_type IN ('shell', 'browser')),
			CONSTRAINT shell_sessions_lease_consistency CHECK (
				(lease_owner_id IS NULL AND lease_until IS NULL)
				OR (lease_owner_id IS NOT NULL AND lease_until IS NOT NULL)
			)
		)`,
		`CREATE INDEX idx_shell_sessions_account_thread ON shell_sessions (account_id, thread_id)`,
		`CREATE INDEX idx_shell_sessions_account_workspace ON shell_sessions (account_id, workspace_ref)`,
		`CREATE INDEX idx_shell_sessions_account_run ON shell_sessions (account_id, run_id)`,
		`CREATE INDEX idx_shell_sessions_account_run_type ON shell_sessions (account_id, run_id, session_type)`,
		`CREATE INDEX idx_shell_sessions_account_lease_until
		    ON shell_sessions (account_id, lease_until)
		    WHERE lease_until IS NOT NULL`,
		`CREATE UNIQUE INDEX idx_shell_sessions_account_profile_binding_type_unique
			    ON shell_sessions (account_id, profile_ref, session_type, default_binding_key)
			    WHERE default_binding_key IS NOT NULL
			      AND state <> 'closed'`,
		`CREATE TABLE profile_registries (
			profile_ref             TEXT        PRIMARY KEY,
			account_id                  UUID        NOT NULL,
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
		`CREATE INDEX idx_profile_registries_account_id ON profile_registries (account_id)`,
		`CREATE TABLE browser_state_registries (
			workspace_ref           TEXT        PRIMARY KEY,
			account_id                  UUID        NOT NULL,
			owner_user_id           UUID        NULL,
			latest_manifest_rev     TEXT        NULL,
			lease_holder_id         TEXT        NULL,
			lease_until             TIMESTAMPTZ NULL,
			store_key               TEXT        NULL,
			flush_state             TEXT        NOT NULL DEFAULT 'idle',
			flush_retry_count       INTEGER     NOT NULL DEFAULT 0,
			last_used_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
			last_flush_failed_at    TIMESTAMPTZ NULL,
			last_flush_succeeded_at TIMESTAMPTZ NULL,
			metadata_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT browser_state_registries_lease_consistency CHECK (
				(lease_holder_id IS NULL AND lease_until IS NULL)
				OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
			)
		)`,
		`CREATE INDEX idx_browser_state_registries_account_id ON browser_state_registries (account_id)`,
		`CREATE TABLE workspace_registries (
			workspace_ref             TEXT        PRIMARY KEY,
			account_id                    UUID        NOT NULL,
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
		`CREATE INDEX idx_workspace_registries_account_id ON workspace_registries (account_id)`,
		`CREATE TABLE skill_packages (
			account_id           UUID        NOT NULL,
			skill_key        TEXT        NOT NULL,
			version          TEXT        NOT NULL,
			display_name     TEXT        NOT NULL,
			description      TEXT        NULL,
			instruction_path TEXT        NOT NULL DEFAULT 'SKILL.md',
			manifest_key     TEXT        NOT NULL,
			bundle_key       TEXT        NOT NULL,
			files_prefix     TEXT        NOT NULL,
			platforms        TEXT[]      NOT NULL DEFAULT '{}',
			is_active        BOOLEAN     NOT NULL DEFAULT TRUE,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (account_id, skill_key, version)
		)`,
		`CREATE TABLE profile_skill_installs (
			profile_ref      TEXT        NOT NULL,
			account_id           UUID        NOT NULL,
			owner_user_id    UUID        NOT NULL,
			skill_key        TEXT        NOT NULL,
			version          TEXT        NOT NULL,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (profile_ref, skill_key, version)
		)`,
		`CREATE TABLE workspace_skill_enablements (
			workspace_ref    TEXT        NOT NULL,
			account_id           UUID        NOT NULL,
			enabled_by_user_id UUID      NOT NULL,
			skill_key        TEXT        NOT NULL,
			version          TEXT        NOT NULL,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (workspace_ref, skill_key)
		)`,
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
			account_id             UUID        NOT NULL,
			thread_id          UUID        NOT NULL,
			thread_seq         BIGINT      NOT NULL,
			created_by_user_id UUID        NULL,
			role               TEXT        NOT NULL,
			content            TEXT        NOT NULL,
			content_json       JSONB       NULL,
			metadata_json      JSONB       NOT NULL DEFAULT '{}'::jsonb,
			hidden             BOOLEAN     NOT NULL DEFAULT FALSE,
			compacted          BOOLEAN     NOT NULL DEFAULT FALSE,
			deleted_at         TIMESTAMPTZ NULL,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE OR REPLACE FUNCTION assign_message_thread_seq_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.thread_seq IS NULL THEN
				UPDATE threads
				   SET next_message_seq = next_message_seq + 1
				 WHERE id = NEW.thread_id
				   AND account_id = NEW.account_id
				RETURNING next_message_seq - 1 INTO NEW.thread_seq;
				IF NEW.thread_seq IS NULL THEN
					RAISE EXCEPTION 'thread % for account % does not exist', NEW.thread_id, NEW.account_id;
				END IF;
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER trg_messages_assign_thread_seq_test
		    BEFORE INSERT ON messages
		    FOR EACH ROW
		    EXECUTE FUNCTION assign_message_thread_seq_test()`,
		`CREATE TABLE user_memory_snapshots (
			account_id       UUID        NOT NULL,
			user_id      UUID        NOT NULL,
			agent_id     TEXT        NOT NULL DEFAULT 'default',
			memory_block TEXT        NOT NULL,
			hits_json    JSONB       NULL,
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (account_id, user_id, agent_id)
		)`,
		`CREATE TABLE usage_records (
			id                    UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id                UUID           NOT NULL,
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
		`CREATE INDEX idx_usage_records_account_recorded ON usage_records (account_id, recorded_at)`,
		`CREATE TABLE credits (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id     UUID        NOT NULL UNIQUE,
			balance    BIGINT      NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE credit_transactions (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id         UUID        NOT NULL,
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
