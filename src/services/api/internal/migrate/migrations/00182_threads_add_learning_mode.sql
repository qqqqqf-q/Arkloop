-- +goose Up
ALTER TABLE threads ADD COLUMN learning_mode_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE threads DROP COLUMN IF EXISTS learning_mode_enabled;
