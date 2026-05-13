-- +goose Up
ALTER TABLE threads
    ADD COLUMN IF NOT EXISTS config_json JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE threads
    DROP COLUMN IF EXISTS config_json;
