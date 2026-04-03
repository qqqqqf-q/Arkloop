-- +goose Up
ALTER TABLE personas ADD COLUMN heartbeat_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE personas ADD COLUMN heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30;

-- +goose Down
ALTER TABLE personas DROP COLUMN heartbeat_enabled;
ALTER TABLE personas DROP COLUMN heartbeat_interval_minutes;
