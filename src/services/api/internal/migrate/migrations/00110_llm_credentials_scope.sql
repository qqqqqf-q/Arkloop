-- +goose Up

ALTER TABLE llm_credentials
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'org';

ALTER TABLE llm_credentials
    ALTER COLUMN org_id DROP NOT NULL;

ALTER TABLE llm_credentials
    DROP CONSTRAINT IF EXISTS uq_llm_credentials_org_name;

CREATE UNIQUE INDEX IF NOT EXISTS llm_credentials_org_name_idx
    ON llm_credentials (org_id, name)
    WHERE scope = 'org';

CREATE UNIQUE INDEX IF NOT EXISTS llm_credentials_platform_name_idx
    ON llm_credentials (name)
    WHERE scope = 'platform';

ALTER TABLE llm_routes
    ALTER COLUMN org_id DROP NOT NULL;

-- +goose Down

ALTER TABLE llm_routes
    ALTER COLUMN org_id SET NOT NULL;

DROP INDEX IF EXISTS llm_credentials_platform_name_idx;
DROP INDEX IF EXISTS llm_credentials_org_name_idx;

ALTER TABLE llm_credentials
    ADD CONSTRAINT uq_llm_credentials_org_name UNIQUE (org_id, name);

ALTER TABLE llm_credentials
    ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE llm_credentials
    DROP COLUMN IF EXISTS scope;
