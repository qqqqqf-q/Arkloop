-- +goose Up

CREATE TABLE prompt_templates (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    content      TEXT        NOT NULL,
    variables    JSONB       NOT NULL DEFAULT '[]',
    is_default   BOOLEAN     NOT NULL DEFAULT false,
    version      INT         NOT NULL DEFAULT 1,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_prompt_templates_org_id ON prompt_templates(org_id);

CREATE TABLE agent_configs (
    id                        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                    UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name                      TEXT        NOT NULL,
    system_prompt_template_id UUID        REFERENCES prompt_templates(id) ON DELETE SET NULL,
    system_prompt_override    TEXT,
    model                     TEXT,
    temperature               DOUBLE PRECISION,
    max_output_tokens         INT,
    top_p                     DOUBLE PRECISION,
    context_window_limit      INT,
    tool_policy               TEXT        NOT NULL DEFAULT 'allowlist'
                              CHECK (tool_policy IN ('allowlist', 'denylist', 'none')),
    tool_allowlist            TEXT[]      NOT NULL DEFAULT '{}',
    tool_denylist             TEXT[]      NOT NULL DEFAULT '{}',
    content_filter_level      TEXT        NOT NULL DEFAULT 'standard',
    safety_rules_json         JSONB       NOT NULL DEFAULT '{}',
    project_id                UUID        REFERENCES projects(id) ON DELETE CASCADE,
    skill_id                  UUID        REFERENCES skills(id) ON DELETE SET NULL,
    is_default                BOOLEAN     NOT NULL DEFAULT false,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_configs_org_id ON agent_configs(org_id);
CREATE INDEX idx_agent_configs_project_id ON agent_configs(project_id) WHERE project_id IS NOT NULL;

ALTER TABLE threads
    ADD COLUMN agent_config_id UUID REFERENCES agent_configs(id) ON DELETE SET NULL;

-- +goose Down

ALTER TABLE threads DROP COLUMN IF EXISTS agent_config_id;
DROP TABLE IF EXISTS agent_configs;
DROP TABLE IF EXISTS prompt_templates;
