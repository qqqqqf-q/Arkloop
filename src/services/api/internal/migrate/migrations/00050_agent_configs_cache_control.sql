-- +goose Up

ALTER TABLE agent_configs
    ADD COLUMN prompt_cache_control TEXT NOT NULL DEFAULT 'none';

-- +goose Down

ALTER TABLE agent_configs
    DROP COLUMN IF EXISTS prompt_cache_control;
