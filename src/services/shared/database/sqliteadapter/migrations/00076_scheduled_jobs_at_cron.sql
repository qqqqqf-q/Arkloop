-- +goose Up
ALTER TABLE scheduled_jobs ADD COLUMN fire_at DATETIME;
ALTER TABLE scheduled_jobs ADD COLUMN cron_expr TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE scheduled_jobs DROP COLUMN cron_expr;
ALTER TABLE scheduled_jobs DROP COLUMN fire_at;
