-- +goose Up
ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS soul_md TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS user_selectable BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS selector_name TEXT,
    ADD COLUMN IF NOT EXISTS selector_order INTEGER,
    ADD COLUMN IF NOT EXISTS title_summarize_json JSONB,
    ADD COLUMN IF NOT EXISTS sync_mode TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS mirrored_file_dir TEXT,
    ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ;

UPDATE personas
SET updated_at = created_at
WHERE updated_at IS NULL;

ALTER TABLE personas
    DROP CONSTRAINT IF EXISTS chk_personas_sync_mode;

ALTER TABLE personas
    ADD CONSTRAINT chk_personas_sync_mode
        CHECK (sync_mode IN ('none', 'platform_file_mirror'));

DELETE FROM personas
WHERE executor_type = 'agent.lua'
  AND COALESCE(executor_config_json ? 'script', FALSE) = FALSE
  AND COALESCE(executor_config_json ? 'script_file', FALSE) = TRUE;

-- +goose Down
ALTER TABLE personas DROP CONSTRAINT IF EXISTS chk_personas_sync_mode;

ALTER TABLE personas
    DROP COLUMN IF EXISTS last_synced_at,
    DROP COLUMN IF EXISTS mirrored_file_dir,
    DROP COLUMN IF EXISTS sync_mode,
    DROP COLUMN IF EXISTS title_summarize_json,
    DROP COLUMN IF EXISTS selector_order,
    DROP COLUMN IF EXISTS selector_name,
    DROP COLUMN IF EXISTS user_selectable,
    DROP COLUMN IF EXISTS soul_md,
    DROP COLUMN IF EXISTS updated_at;
