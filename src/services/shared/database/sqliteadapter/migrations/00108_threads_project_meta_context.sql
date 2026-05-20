-- +goose Up
ALTER TABLE threads ADD COLUMN project_meta_context TEXT DEFAULT NULL;

-- +goose Down

