-- +goose Up
ALTER TABLE scheduled_jobs ADD COLUMN weekly_day INT;

-- +goose Down
ALTER TABLE scheduled_jobs DROP COLUMN IF EXISTS weekly_day;
