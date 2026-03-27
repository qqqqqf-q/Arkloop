-- +goose Up
ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS conditional_tools_json JSONB;

-- +goose Down
ALTER TABLE personas
    DROP COLUMN IF EXISTS conditional_tools_json;
