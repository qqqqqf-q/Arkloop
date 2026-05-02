-- +goose Up
ALTER TABLE threads ADD COLUMN learning_mode_enabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE threads DROP COLUMN learning_mode_enabled;
