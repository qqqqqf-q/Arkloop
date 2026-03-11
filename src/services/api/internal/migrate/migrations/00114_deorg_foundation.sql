-- +goose Up

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS project_settings (
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key         TEXT        NOT NULL,
    value       TEXT        NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, key)
);

CREATE INDEX IF NOT EXISTS ix_project_settings_key ON project_settings(key);

ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX IF NOT EXISTS uq_personas_project_key_version
    ON personas (project_id, persona_key, version)
    WHERE project_id IS NOT NULL;

ALTER TABLE secrets
    ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE secrets DROP CONSTRAINT IF EXISTS chk_secrets_owner_kind;
ALTER TABLE secrets
    ADD CONSTRAINT chk_secrets_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX IF NOT EXISTS secrets_user_name_idx
    ON secrets (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

ALTER TABLE llm_credentials
    ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE llm_credentials DROP CONSTRAINT IF EXISTS chk_llm_credentials_owner_kind;
ALTER TABLE llm_credentials
    ADD CONSTRAINT chk_llm_credentials_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX IF NOT EXISTS llm_credentials_user_name_idx
    ON llm_credentials (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

ALTER TABLE asr_credentials
    ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE asr_credentials DROP CONSTRAINT IF EXISTS chk_asr_credentials_owner_kind;
ALTER TABLE asr_credentials
    ADD CONSTRAINT chk_asr_credentials_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX IF NOT EXISTS asr_credentials_user_name_idx
    ON asr_credentials (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

ALTER TABLE llm_routes
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS route_key TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT;

UPDATE llm_routes
SET route_key = id::TEXT
WHERE NULLIF(BTRIM(route_key), '') IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_llm_routes_route_key
    ON llm_routes (LOWER(route_key));

CREATE INDEX IF NOT EXISTS ix_llm_routes_project_id ON llm_routes(project_id) WHERE project_id IS NOT NULL;

ALTER TABLE tool_provider_configs
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS ix_tool_provider_configs_project_group_active
    ON tool_provider_configs (project_id, group_name)
    WHERE project_id IS NOT NULL AND is_active = TRUE;

ALTER TABLE tool_description_overrides
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS ix_tool_description_overrides_project_tool
    ON tool_description_overrides (project_id, tool_name)
    WHERE project_id IS NOT NULL;

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

UPDATE runs AS r
SET project_id = t.project_id
FROM threads AS t
WHERE t.id = r.thread_id
  AND r.project_id IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arkloop_runs_fill_project_id()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id IS NULL AND NEW.thread_id IS NOT NULL THEN
        SELECT project_id INTO NEW.project_id
        FROM threads
        WHERE id = NEW.thread_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_runs_fill_project_id ON runs;
CREATE TRIGGER trg_runs_fill_project_id
    BEFORE INSERT OR UPDATE OF thread_id, project_id ON runs
    FOR EACH ROW
    EXECUTE FUNCTION arkloop_runs_fill_project_id();

CREATE INDEX IF NOT EXISTS ix_runs_project_id_created_at_id
    ON runs (project_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL AND project_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS ix_runs_project_id_created_at_id;
DROP TRIGGER IF EXISTS trg_runs_fill_project_id ON runs;
DROP FUNCTION IF EXISTS arkloop_runs_fill_project_id();

ALTER TABLE runs
    DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS ix_tool_description_overrides_project_tool;
ALTER TABLE tool_description_overrides
    DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS ix_tool_provider_configs_project_group_active;
ALTER TABLE tool_provider_configs
    DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS ix_llm_routes_project_id;
DROP INDEX IF EXISTS ux_llm_routes_route_key;
ALTER TABLE llm_routes
    DROP COLUMN IF EXISTS route_key,
    DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS asr_credentials_user_name_idx;
ALTER TABLE asr_credentials DROP CONSTRAINT IF EXISTS chk_asr_credentials_owner_kind;
ALTER TABLE asr_credentials
    DROP COLUMN IF EXISTS owner_user_id,
    DROP COLUMN IF EXISTS owner_kind;

DROP INDEX IF EXISTS llm_credentials_user_name_idx;
ALTER TABLE llm_credentials DROP CONSTRAINT IF EXISTS chk_llm_credentials_owner_kind;
ALTER TABLE llm_credentials
    DROP COLUMN IF EXISTS owner_user_id,
    DROP COLUMN IF EXISTS owner_kind;

DROP INDEX IF EXISTS secrets_user_name_idx;
ALTER TABLE secrets DROP CONSTRAINT IF EXISTS chk_secrets_owner_kind;
ALTER TABLE secrets
    DROP COLUMN IF EXISTS owner_user_id,
    DROP COLUMN IF EXISTS owner_kind;

DROP INDEX IF EXISTS uq_personas_project_key_version;
ALTER TABLE personas
    DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS ix_project_settings_key;
DROP TABLE IF EXISTS project_settings;

ALTER TABLE users
    DROP COLUMN IF EXISTS is_platform_admin;
