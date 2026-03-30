-- +goose Up
ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS result_summarize_json JSONB;

-- +goose Down
ALTER TABLE personas
    DROP COLUMN IF EXISTS result_summarize_json;
