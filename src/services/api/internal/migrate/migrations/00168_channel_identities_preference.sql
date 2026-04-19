-- +goose Up
ALTER TABLE channel_identities
    ADD COLUMN IF NOT EXISTS preferred_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reasoning_mode TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE channel_identities
    DROP COLUMN IF EXISTS preferred_model,
    DROP COLUMN IF EXISTS reasoning_mode;
