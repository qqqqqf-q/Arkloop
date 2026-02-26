-- +goose Up
ALTER TABLE skills
    ADD COLUMN executor_type TEXT NOT NULL DEFAULT 'agent.simple'
        CHECK (executor_type ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
    ADD COLUMN executor_config_json JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE skills
    DROP COLUMN IF EXISTS executor_type,
    DROP COLUMN IF EXISTS executor_config_json;
