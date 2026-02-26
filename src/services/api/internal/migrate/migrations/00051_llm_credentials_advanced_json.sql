-- +goose Up

ALTER TABLE llm_credentials ADD COLUMN advanced_json JSONB NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE llm_credentials DROP COLUMN IF EXISTS advanced_json;
