-- +goose Up
ALTER TABLE scheduled_jobs ADD COLUMN weekly_day INTEGER;

-- +goose Down
-- SQLite 不支持 DROP COLUMN
