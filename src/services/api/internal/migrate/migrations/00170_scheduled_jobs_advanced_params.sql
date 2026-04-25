-- +goose Up
ALTER TABLE scheduled_jobs ADD COLUMN delete_after_run BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE scheduled_jobs ADD COLUMN thinking BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE scheduled_jobs ADD COLUMN timeout_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_jobs ADD COLUMN light_context BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE scheduled_jobs ADD COLUMN tools_allow TEXT NOT NULL DEFAULT '';
