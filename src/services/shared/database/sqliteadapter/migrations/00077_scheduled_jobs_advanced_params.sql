-- +goose Up
ALTER TABLE scheduled_jobs ADD COLUMN delete_after_run INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_jobs ADD COLUMN thinking INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_jobs ADD COLUMN timeout_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_jobs ADD COLUMN light_context INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_jobs ADD COLUMN tools_allow TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE scheduled_jobs DROP COLUMN tools_allow;
ALTER TABLE scheduled_jobs DROP COLUMN light_context;
ALTER TABLE scheduled_jobs DROP COLUMN timeout_seconds;
ALTER TABLE scheduled_jobs DROP COLUMN thinking;
ALTER TABLE scheduled_jobs DROP COLUMN delete_after_run;
