-- +goose Up
ALTER TABLE threads ADD COLUMN IF NOT EXISTS project_meta_context JSONB DEFAULT NULL;

-- +goose Down
ALTER TABLE threads DROP COLUMN IF EXISTS project_meta_context;
