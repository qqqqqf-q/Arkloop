-- +goose Up
ALTER TABLE scheduled_triggers ADD COLUMN cooldown_level INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite 不支持 DROP COLUMN，这里为空或重建表
