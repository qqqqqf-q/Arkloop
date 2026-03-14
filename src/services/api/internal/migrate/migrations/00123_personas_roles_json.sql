-- +goose Up

ALTER TABLE personas
    ADD COLUMN roles_json JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down

ALTER TABLE personas
    DROP COLUMN IF EXISTS roles_json;
