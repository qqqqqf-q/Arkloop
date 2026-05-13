-- +goose Up
ALTER TABLE threads ADD COLUMN config_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE threads DROP COLUMN config_json;
