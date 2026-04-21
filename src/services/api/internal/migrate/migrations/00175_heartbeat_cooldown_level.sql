-- +goose Up
ALTER TABLE scheduled_triggers
    ADD COLUMN IF NOT EXISTS cooldown_level INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_user_msg_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS burst_start_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE scheduled_triggers
    DROP COLUMN IF EXISTS cooldown_level,
    DROP COLUMN IF EXISTS last_user_msg_at,
    DROP COLUMN IF EXISTS burst_start_at;
