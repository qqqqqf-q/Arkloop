-- +goose Up

ALTER TABLE agent_configs
    ADD COLUMN scope TEXT NOT NULL DEFAULT 'org';

ALTER TABLE agent_configs
    ALTER COLUMN org_id DROP NOT NULL;

-- 现有数据全部是 org 级，不需要回填

-- +goose Down

ALTER TABLE agent_configs
    ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE agent_configs
    DROP COLUMN IF EXISTS scope;
