-- +goose Up
ALTER TABLE personas
    ADD COLUMN soul_md              TEXT    NOT NULL DEFAULT '',
    ADD COLUMN user_selectable      BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN selector_name        TEXT    NULL,
    ADD COLUMN selector_order       INTEGER NULL,
    ADD COLUMN title_summarize_json JSONB   NULL,
    ADD COLUMN updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN sync_mode            TEXT    NOT NULL DEFAULT '',
    ADD COLUMN mirrored_file_dir    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN last_synced_at       TIMESTAMPTZ NULL;

-- +goose Down
ALTER TABLE personas
    DROP COLUMN IF EXISTS soul_md,
    DROP COLUMN IF EXISTS user_selectable,
    DROP COLUMN IF EXISTS selector_name,
    DROP COLUMN IF EXISTS selector_order,
    DROP COLUMN IF EXISTS title_summarize_json,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS sync_mode,
    DROP COLUMN IF EXISTS mirrored_file_dir,
    DROP COLUMN IF EXISTS last_synced_at;
