-- +goose Up
ALTER TABLE scheduled_triggers
    ADD COLUMN cooldown_level INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE scheduled_triggers DROP COLUMN cooldown_level;
