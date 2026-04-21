-- +goose Up
ALTER TABLE scheduled_triggers
    ADD COLUMN cooldown_level INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_triggers
    ADD COLUMN last_user_msg_at TEXT;
ALTER TABLE scheduled_triggers
    ADD COLUMN burst_start_at TEXT;

-- +goose Down
-- SQLite 不支持 DROP COLUMN，这里为空或重建表
