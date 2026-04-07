-- PostgreSQL schema snapshot
-- Extracted from migration Up sections.
-- This is an approximation; for exact schema, run migrations against a live PG instance.
-- Do NOT edit manually. Regenerate after adding migrations.

-- === 00001_init_empty.sql ===
-- empty bootstrap migration


-- === 00002_create_orgs_users_memberships.sql ===
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE orgs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT uq_orgs_slug UNIQUE (slug)
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE TABLE org_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT uq_org_memberships_org_id_user_id UNIQUE (org_id, user_id)
);

CREATE INDEX ix_org_memberships_org_id ON org_memberships(org_id);
CREATE INDEX ix_org_memberships_user_id ON org_memberships(user_id);


-- === 00003_create_threads_messages_runs_events.sql ===
CREATE TABLE threads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    title TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_threads_org_id ON threads(org_id);
CREATE INDEX ix_threads_created_by_user_id ON threads(created_by_user_id);

CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_messages_thread_id ON messages(thread_id);

CREATE TABLE runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'running',
    next_event_seq BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_runs_org_id ON runs(org_id);
CREATE INDEX ix_runs_thread_id ON runs(thread_id);

CREATE TABLE run_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq BIGINT NOT NULL,
    ts TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    type TEXT NOT NULL,
    data_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    tool_name TEXT,
    error_class TEXT,
    CONSTRAINT uq_run_events_run_id_seq UNIQUE (run_id, seq)
);

CREATE INDEX ix_run_events_type ON run_events(type);
CREATE INDEX ix_run_events_tool_name ON run_events(tool_name);
CREATE INDEX ix_run_events_error_class ON run_events(error_class);


-- === 00004_messages_org_consistency.sql ===
ALTER TABLE messages ADD COLUMN org_id UUID;
ALTER TABLE messages ADD COLUMN created_by_user_id UUID;

-- backfill org_id from threads (no-op on fresh database)
UPDATE messages AS m
SET org_id = t.org_id
FROM threads AS t
WHERE m.thread_id = t.id
  AND m.org_id IS NULL;

ALTER TABLE messages ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE threads ADD CONSTRAINT uq_threads_id_org_id UNIQUE (id, org_id);
ALTER TABLE messages DROP CONSTRAINT messages_thread_id_fkey;

ALTER TABLE messages ADD CONSTRAINT fk_messages_org_id_orgs
    FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;

ALTER TABLE messages ADD CONSTRAINT fk_messages_created_by_user_id_users
    FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE messages ADD CONSTRAINT fk_messages_thread_org
    FOREIGN KEY (thread_id, org_id) REFERENCES threads(id, org_id) ON DELETE CASCADE;

CREATE INDEX ix_messages_org_id_thread_id_created_at
    ON messages(org_id, thread_id, created_at);


-- === 00005_create_user_credentials.sql ===
CREATE TABLE user_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    login TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT uq_user_credentials_user_id UNIQUE (user_id),
    CONSTRAINT uq_user_credentials_login UNIQUE (login)
);


-- === 00006_create_audit_logs.sql ===
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID REFERENCES orgs(id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target_type TEXT,
    target_id TEXT,
    ts TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    trace_id TEXT NOT NULL,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX ix_audit_logs_trace_id ON audit_logs(trace_id);
CREATE INDEX ix_audit_logs_org_id_ts ON audit_logs(org_id, ts);


-- === 00007_add_users_tokens_invalid_before.sql ===
ALTER TABLE users ADD COLUMN tokens_invalid_before TIMESTAMP WITH TIME ZONE
    NOT NULL DEFAULT to_timestamp(0);


-- === 00008_create_jobs_table.sql ===
CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type TEXT NOT NULL,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'queued',
    available_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    leased_until TIMESTAMP WITH TIME ZONE,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_jobs_job_type ON jobs(job_type);
CREATE INDEX ix_jobs_status_available_at ON jobs(status, available_at);
CREATE INDEX ix_jobs_status_leased_until ON jobs(status, leased_until);
CREATE UNIQUE INDEX ux_jobs_run_execute_active_run ON jobs (((payload_json ->> 'run_id'::text))) WHERE ((job_type = 'run.execute'::text) AND (status = ANY (ARRAY['queued'::text, 'leased'::text])));


-- === 00009_jobs_add_lease_token.sql ===
ALTER TABLE jobs ADD COLUMN lease_token UUID;


-- === 00010_messages_add_hidden.sql ===
ALTER TABLE messages ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX ix_messages_hidden ON messages(hidden) WHERE hidden = TRUE;


-- === 00011_fix_stale_running_runs.sql ===
-- 修复历史遗留的 stale runs：status 仍为 running 但已有 terminal event
UPDATE runs r
SET status = CASE e.type
    WHEN 'run.completed' THEN 'completed'
    WHEN 'run.failed'    THEN 'failed'
    WHEN 'run.cancelled' THEN 'cancelled'
END
FROM (
    SELECT DISTINCT ON (run_id) run_id, type
    FROM run_events
    WHERE type IN ('run.completed', 'run.failed', 'run.cancelled')
    ORDER BY run_id, seq DESC
) e
WHERE r.id = e.run_id
  AND r.status = 'running';


-- === 00012_users_add_email_status.sql ===
ALTER TABLE users
    ADD COLUMN email             TEXT,
    ADD COLUMN email_verified_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN status            TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN deleted_at        TIMESTAMP WITH TIME ZONE,
    ADD COLUMN avatar_url        TEXT,
    ADD COLUMN locale            TEXT,
    ADD COLUMN timezone          TEXT,
    ADD COLUMN last_login_at     TIMESTAMP WITH TIME ZONE;

ALTER TABLE users
    ADD CONSTRAINT chk_users_status CHECK (status IN ('active', 'suspended', 'deleted'));

CREATE UNIQUE INDEX uq_users_email ON users (email) WHERE deleted_at IS NULL;


-- === 00013_runs_add_lifecycle_fields.sql ===
ALTER TABLE runs
    ADD COLUMN parent_run_id        UUID REFERENCES runs(id) ON DELETE SET NULL,
    ADD COLUMN status_updated_at    TIMESTAMP WITH TIME ZONE,
    ADD COLUMN completed_at         TIMESTAMP WITH TIME ZONE,
    ADD COLUMN failed_at            TIMESTAMP WITH TIME ZONE,
    ADD COLUMN duration_ms          BIGINT,
    ADD COLUMN total_input_tokens   BIGINT,
    ADD COLUMN total_output_tokens  BIGINT,
    ADD COLUMN total_cost_usd       NUMERIC(18, 8),
    ADD COLUMN model                TEXT,
    ADD COLUMN skill_id             TEXT,
    ADD COLUMN deleted_at           TIMESTAMP WITH TIME ZONE;

-- 兜底修正非法 status（00011 已处理大部分，此处防御性修正）
UPDATE runs
SET status = 'failed'
WHERE status NOT IN ('running', 'completed', 'failed', 'cancelled', 'cancelling');

ALTER TABLE runs
    ADD CONSTRAINT ck_runs_status
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling'));


-- === 00014_orgs_add_owner_status.sql ===
ALTER TABLE orgs
    ADD COLUMN owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN status        TEXT NOT NULL DEFAULT 'active'
                             CHECK (status IN ('active', 'suspended')),
    ADD COLUMN country       TEXT,
    ADD COLUMN timezone      TEXT,
    ADD COLUMN logo_url      TEXT,
    ADD COLUMN settings_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN deleted_at    TIMESTAMP WITH TIME ZONE;


-- === 00015_run_events_use_sequence.sql ===
CREATE SEQUENCE run_events_seq_global;

-- 将序号对齐到现有最大值，避免与已有数据冲突
-- setval 第三个参数 false：nextval 将返回正好等于该值（即 max+1）
SELECT setval('run_events_seq_global', COALESCE(MAX(seq), 0) + 1, false)
FROM run_events;

ALTER TABLE run_events
    ALTER COLUMN seq SET DEFAULT nextval('run_events_seq_global');


-- === 00016_messages_add_content_json.sql ===
ALTER TABLE messages
    ADD COLUMN content_json   JSONB,
    ADD COLUMN metadata_json  JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN deleted_at     TIMESTAMP WITH TIME ZONE,
    ADD COLUMN token_count    INTEGER;


-- === 00017_threads_add_soft_delete.sql ===
ALTER TABLE threads
    ADD COLUMN deleted_at  TIMESTAMP WITH TIME ZONE,
    ADD COLUMN project_id  UUID;

-- 仅索引已删除的行，未删除的不占用索引空间
CREATE INDEX ix_threads_deleted_at ON threads(deleted_at) WHERE deleted_at IS NOT NULL;


-- === 00018_audit_logs_add_ip_ua_state.sql ===
ALTER TABLE audit_logs
    ADD COLUMN ip_address        INET,
    ADD COLUMN user_agent        TEXT,
    ADD COLUMN api_key_id        UUID,
    ADD COLUMN before_state_json JSONB,
    ADD COLUMN after_state_json  JSONB;


-- === 00019_create_secrets.sql ===
CREATE TABLE secrets (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    encrypted_value TEXT        NOT NULL,
    key_version     INT         NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,
    CONSTRAINT uq_secrets_org_name UNIQUE (org_id, name)
);

CREATE INDEX ix_secrets_org_id ON secrets(org_id);


-- === 00020_create_llm_credentials_routes.sql ===
CREATE TABLE llm_credentials (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider        TEXT        NOT NULL
                                CHECK (provider IN ('openai', 'anthropic', 'gemini', 'deepseek')),
    name            TEXT        NOT NULL,
    secret_id       UUID        REFERENCES secrets(id) ON DELETE SET NULL,
    key_prefix      TEXT,
    base_url        TEXT,
    openai_api_mode TEXT,
    revoked_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_llm_credentials_org_name UNIQUE (org_id, name)
);

CREATE TABLE llm_routes (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    credential_id UUID        NOT NULL REFERENCES llm_credentials(id) ON DELETE CASCADE,
    model         TEXT        NOT NULL,
    priority      INT         NOT NULL DEFAULT 0,
    is_default    BOOLEAN     NOT NULL DEFAULT false,
    when_json     JSONB       NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_llm_credentials_org_id ON llm_credentials(org_id);
CREATE INDEX ix_llm_routes_org_id ON llm_routes(org_id);
CREATE INDEX ix_llm_routes_credential_id ON llm_routes(credential_id);


-- === 00021_create_mcp_configs.sql ===
CREATE TABLE mcp_configs (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             UUID        NOT NULL REFERENCES orgs(id),
    name               TEXT        NOT NULL,
    transport          TEXT        NOT NULL,
    url                TEXT,
    auth_secret_id     UUID        REFERENCES secrets(id),
    command            TEXT,
    args_json          JSONB       NOT NULL DEFAULT '[]',
    cwd                TEXT,
    env_json           JSONB       NOT NULL DEFAULT '{}',
    inherit_parent_env BOOLEAN     NOT NULL DEFAULT FALSE,
    call_timeout_ms    INT         NOT NULL DEFAULT 10000,
    is_active          BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_mcp_configs_org_name    UNIQUE (org_id, name),
    CONSTRAINT chk_mcp_configs_transport  CHECK (transport IN ('stdio', 'http_sse', 'streamable_http')),
    CONSTRAINT chk_mcp_configs_timeout    CHECK (call_timeout_ms > 0),
    CONSTRAINT chk_mcp_configs_stdio_cmd  CHECK (transport != 'stdio' OR command IS NOT NULL),
    CONSTRAINT chk_mcp_configs_remote_url CHECK (transport = 'stdio' OR url IS NOT NULL)
);


-- === 00022_mcp_configs_add_org_cascade.sql ===
ALTER TABLE mcp_configs
    DROP CONSTRAINT mcp_configs_org_id_fkey,
    ADD CONSTRAINT mcp_configs_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;


-- === 00023_create_skills.sql ===
CREATE TABLE skills (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        REFERENCES orgs(id),
    skill_key      TEXT        NOT NULL,
    version        TEXT        NOT NULL,
    display_name   TEXT        NOT NULL,
    description    TEXT,
    prompt_md      TEXT        NOT NULL,
    tool_allowlist TEXT[]      NOT NULL DEFAULT '{}',
    budgets_json   JSONB       NOT NULL DEFAULT '{}',
    is_active      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_skills_org_key_version UNIQUE NULLS NOT DISTINCT (org_id, skill_key, version),
    CONSTRAINT chk_skills_key_format     CHECK (skill_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
    CONSTRAINT chk_skills_version_format CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$')
);


-- === 00024_create_worker_registrations.sql ===
CREATE TABLE worker_registrations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    worker_id       UUID        NOT NULL UNIQUE,
    hostname        TEXT        NOT NULL,
    version         TEXT        NOT NULL DEFAULT 'unknown',
    status          TEXT        NOT NULL DEFAULT 'active',
    capabilities    JSONB       NOT NULL DEFAULT '[]',
    current_load    INT         NOT NULL DEFAULT 0,
    max_concurrency INT         NOT NULL DEFAULT 4,
    heartbeat_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_worker_status CHECK (status IN ('active', 'draining', 'dead'))
);

CREATE INDEX idx_worker_registrations_status    ON worker_registrations (status);
CREATE INDEX idx_worker_registrations_heartbeat ON worker_registrations (heartbeat_at);


-- === 00025_jobs_add_worker_tags.sql ===
ALTER TABLE jobs ADD COLUMN worker_tags TEXT[] NOT NULL DEFAULT '{}';


-- === 00026_create_ip_rules.sql ===
CREATE TABLE ip_rules (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    type       TEXT        NOT NULL,
    cidr       CIDR        NOT NULL,
    note       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_ip_rules_type CHECK (type IN ('allowlist', 'blocklist'))
);

CREATE INDEX idx_ip_rules_org_id ON ip_rules (org_id);


-- === 00027_create_api_keys.sql ===
CREATE TABLE api_keys (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    key_prefix   TEXT        NOT NULL,
    key_hash     TEXT        NOT NULL UNIQUE,
    scopes       TEXT[]      NOT NULL DEFAULT '{}',
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_org_id ON api_keys (org_id);
CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash);


-- === 00028_create_org_invitations.sql ===
CREATE TABLE org_invitations (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    invited_by_user_id  UUID        NOT NULL REFERENCES users(id),
    email               TEXT        NOT NULL,
    role                TEXT        NOT NULL DEFAULT 'member',
    token               TEXT        NOT NULL UNIQUE,
    expires_at          TIMESTAMPTZ NOT NULL,
    accepted_at         TIMESTAMPTZ,
    accepted_by_user_id UUID        REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_org_invitations_org_id ON org_invitations(org_id) WHERE accepted_at IS NULL;
CREATE INDEX idx_org_invitations_token  ON org_invitations(token);


-- === 00029_create_rbac_roles.sql ===

CREATE TABLE rbac_roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    permissions TEXT[]      NOT NULL DEFAULT '{}',
    is_system   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE UNIQUE INDEX idx_rbac_roles_system_name ON rbac_roles(name) WHERE org_id IS NULL;

-- 内置系统角色
INSERT INTO rbac_roles (name, permissions, is_system) VALUES
(
    'platform_admin',
    ARRAY[
        'platform.admin',
        'org.members.invite', 'org.members.list', 'org.members.revoke',
        'data.threads.read', 'data.threads.write',
        'data.runs.read', 'data.runs.write',
        'data.api_keys.manage',
        'data.skills.read',
        'data.llm_credentials.manage',
        'data.mcp_configs.manage',
        'data.secrets.manage'
    ],
    TRUE
),
(
    'org_admin',
    ARRAY[
        'org.members.invite', 'org.members.list', 'org.members.revoke',
        'data.threads.read', 'data.threads.write',
        'data.runs.read', 'data.runs.write',
        'data.api_keys.manage',
        'data.skills.read',
        'data.llm_credentials.manage',
        'data.mcp_configs.manage',
        'data.secrets.manage'
    ],
    TRUE
),
(
    'org_member',
    ARRAY[
        'data.threads.read', 'data.threads.write',
        'data.runs.read', 'data.runs.write',
        'data.api_keys.manage',
        'data.skills.read'
    ],
    TRUE
);

ALTER TABLE org_memberships ADD COLUMN role_id UUID REFERENCES rbac_roles(id);

UPDATE org_memberships m
SET role_id = r.id
FROM rbac_roles r
WHERE r.name = 'org_admin'
  AND r.org_id IS NULL
  AND m.role = 'owner';

UPDATE org_memberships m
SET role_id = r.id
FROM rbac_roles r
WHERE r.name = 'org_member'
  AND r.org_id IS NULL
  AND m.role = 'member';


-- === 00030_create_teams_projects.sql ===

CREATE TABLE teams (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_teams_org_id ON teams(org_id);

CREATE TABLE team_memberships (
    team_id    UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT        NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, user_id)
);

CREATE INDEX idx_team_memberships_user_id ON team_memberships(user_id);

CREATE TABLE projects (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    team_id     UUID        REFERENCES teams(id) ON DELETE SET NULL,
    name        TEXT        NOT NULL,
    description TEXT,
    visibility  TEXT        NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'team', 'org')),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_projects_org_id ON projects(org_id);
CREATE INDEX idx_projects_team_id ON projects(team_id) WHERE team_id IS NOT NULL;

ALTER TABLE threads
    ADD CONSTRAINT fk_threads_project_id
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;


-- === 00031_create_webhooks.sql ===

CREATE TABLE webhook_endpoints (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    url            TEXT        NOT NULL,
    signing_secret TEXT        NOT NULL,
    events         TEXT[]      NOT NULL DEFAULT '{}',
    enabled        BOOLEAN     NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_endpoints_org_id ON webhook_endpoints(org_id);

CREATE TABLE webhook_deliveries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id     UUID        NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    org_id          UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    event_type      TEXT        NOT NULL,
    payload_json    JSONB       NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    response_status INT,
    response_body   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_deliveries_endpoint_id ON webhook_deliveries(endpoint_id);
CREATE INDEX idx_webhook_deliveries_org_id ON webhook_deliveries(org_id);


-- === 00032_create_agent_configs.sql ===

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


-- === 00033_create_plans_and_entitlements.sql ===

CREATE TABLE plans (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT        NOT NULL UNIQUE,
    display_name TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plan_entitlements (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id    UUID NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    value_type TEXT NOT NULL CHECK (value_type IN ('int', 'bool', 'string')),
    UNIQUE (plan_id, key)
);

CREATE INDEX idx_plan_entitlements_plan_id ON plan_entitlements(plan_id);

CREATE TABLE subscriptions (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id               UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    plan_id              UUID        NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    status               TEXT        NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active', 'cancelled', 'expired')),
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end   TIMESTAMPTZ NOT NULL,
    cancelled_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_subscriptions_org_active
    ON subscriptions(org_id) WHERE status = 'active';

CREATE INDEX idx_subscriptions_plan_id ON subscriptions(plan_id);

CREATE TABLE org_entitlement_overrides (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    key                TEXT        NOT NULL,
    value              TEXT        NOT NULL,
    value_type         TEXT        NOT NULL CHECK (value_type IN ('int', 'bool', 'string')),
    reason             TEXT,
    expires_at         TIMESTAMPTZ,
    created_by_user_id UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, key)
);

CREATE INDEX idx_org_entitlement_overrides_org_id ON org_entitlement_overrides(org_id);


-- === 00034_create_usage_records.sql ===

CREATE TABLE usage_records (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    run_id         UUID        NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,
    model          TEXT        NOT NULL DEFAULT '',
    input_tokens   BIGINT      NOT NULL DEFAULT 0,
    output_tokens  BIGINT      NOT NULL DEFAULT 0,
    cost_usd       NUMERIC(18, 8) NOT NULL DEFAULT 0,
    recorded_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_usage_records_org_recorded ON usage_records(org_id, recorded_at);


-- === 00035_create_feature_flags.sql ===

CREATE TABLE feature_flags (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    key           TEXT        NOT NULL UNIQUE,
    description   TEXT,
    default_value BOOLEAN     NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE org_feature_overrides (
    org_id     UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    flag_key   TEXT        NOT NULL,
    enabled    BOOLEAN     NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, flag_key)
);

CREATE INDEX idx_org_feature_overrides_org_id ON org_feature_overrides(org_id);


-- === 00036_create_notifications.sql ===

CREATE TABLE notifications (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id       UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    type         TEXT        NOT NULL,
    title        TEXT        NOT NULL,
    body         TEXT        NOT NULL DEFAULT '',
    payload_json JSONB       NOT NULL DEFAULT '{}'::jsonb,
    read_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_user_id_unread ON notifications(user_id) WHERE read_at IS NULL;


-- === 00037_runs_add_org_created_at_index.sql ===

CREATE INDEX ix_runs_org_id_created_at_id ON runs(org_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;


-- === 00038_create_invite_codes_and_referrals.sql ===

CREATE TABLE invite_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT        NOT NULL UNIQUE,
    max_uses   INT         NOT NULL,
    use_count  INT         NOT NULL DEFAULT 0,
    is_active  BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invite_codes_user_id ON invite_codes(user_id);

CREATE TABLE referrals (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    inviter_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invitee_user_id UUID        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    invite_code_id  UUID        NOT NULL REFERENCES invite_codes(id) ON DELETE CASCADE,
    credited        BOOLEAN     NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_referrals_inviter_user_id ON referrals(inviter_user_id, created_at DESC);


-- === 00039_credits_and_route_pricing.sql ===

ALTER TABLE llm_routes ADD COLUMN multiplier DOUBLE PRECISION NOT NULL DEFAULT 1.0;
ALTER TABLE llm_routes ADD COLUMN cost_per_1k_input DOUBLE PRECISION;
ALTER TABLE llm_routes ADD COLUMN cost_per_1k_output DOUBLE PRECISION;

CREATE TABLE credits (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID        NOT NULL UNIQUE REFERENCES orgs(id) ON DELETE CASCADE,
    balance    BIGINT      NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE credit_transactions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    amount         BIGINT      NOT NULL,
    type           TEXT        NOT NULL,
    reference_type TEXT,
    reference_id   UUID,
    note           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_credit_transactions_org_created ON credit_transactions(org_id, created_at DESC);


-- === 00040_create_redemption_codes.sql ===

CREATE TABLE redemption_codes (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code               TEXT        NOT NULL UNIQUE,
    type               TEXT        NOT NULL CHECK (type IN ('credit', 'feature')),
    value              TEXT        NOT NULL,
    max_uses           INT         NOT NULL DEFAULT 1,
    use_count          INT         NOT NULL DEFAULT 0,
    expires_at         TIMESTAMPTZ,
    is_active          BOOLEAN     NOT NULL DEFAULT true,
    batch_id           TEXT,
    created_by_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_redemption_codes_batch_id ON redemption_codes(batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX idx_redemption_codes_created_at ON redemption_codes(created_at DESC, id DESC);

CREATE TABLE redemption_records (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code_id     UUID        NOT NULL REFERENCES redemption_codes(id) ON DELETE CASCADE,
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(code_id, user_id)
);

CREATE INDEX idx_redemption_records_user_id ON redemption_records(user_id);


-- === 00041_create_notification_broadcasts.sql ===

CREATE TABLE notification_broadcasts (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT        NOT NULL,
    title           TEXT        NOT NULL,
    body            TEXT        NOT NULL DEFAULT '',
    target_type     TEXT        NOT NULL DEFAULT 'all',
    target_id       UUID,
    payload_json    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT        NOT NULL DEFAULT 'pending',
    sent_count      INT         NOT NULL DEFAULT 0,
    created_by      UUID        NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notification_broadcasts_created_at ON notification_broadcasts(created_at DESC);

ALTER TABLE notifications ADD COLUMN broadcast_id UUID REFERENCES notification_broadcasts(id);
CREATE INDEX idx_notifications_broadcast_id ON notifications(broadcast_id) WHERE broadcast_id IS NOT NULL;


-- === 00042_create_platform_settings.sql ===

CREATE TABLE platform_settings (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);


-- === 00043_create_asr_credentials.sql ===

CREATE TABLE asr_credentials (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider    TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    secret_id   UUID        REFERENCES secrets(id) ON DELETE SET NULL,
    key_prefix  TEXT,
    base_url    TEXT,
    model       TEXT        NOT NULL,
    is_default  BOOLEAN     NOT NULL DEFAULT false,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

-- 每个 org 最多一个 default
CREATE UNIQUE INDEX asr_credentials_org_default_idx
    ON asr_credentials (org_id)
    WHERE is_default = true AND revoked_at IS NULL;


-- === 00044_create_refresh_tokens.sql ===

CREATE TABLE refresh_tokens (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash    TEXT        NOT NULL UNIQUE,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);


-- === 00045_run_events_monthly_partition.sql ===

-- 1. 创建分区父表，结构与现有 run_events 一致
--    PK 和 UNIQUE 必须包含分区键 ts
CREATE TABLE run_events_partitioned (
    event_id    UUID         DEFAULT gen_random_uuid(),
    run_id      UUID         NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq         BIGINT       NOT NULL DEFAULT nextval('run_events_seq_global'),
    ts          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    type        TEXT         NOT NULL,
    data_json   JSONB        NOT NULL DEFAULT '{}'::jsonb,
    tool_name   TEXT,
    error_class TEXT,
    CONSTRAINT pk_run_events_part PRIMARY KEY (event_id, ts),
    CONSTRAINT uq_run_events_part_run_id_seq UNIQUE (run_id, seq, ts)
) PARTITION BY RANGE (ts);

-- 2. DEFAULT 分区兜底历史数据和异常时间戳
CREATE TABLE run_events_pdefault PARTITION OF run_events_partitioned DEFAULT;

-- 3. 动态创建当前月和下月分区
-- +goose StatementBegin
DO $body$
DECLARE
    curr_start DATE := date_trunc('month', now())::DATE;
    next_start DATE := (date_trunc('month', now()) + INTERVAL '1 month')::DATE;
    after_start DATE := (date_trunc('month', now()) + INTERVAL '2 months')::DATE;
BEGIN
    EXECUTE format(
        'CREATE TABLE run_events_p%s PARTITION OF run_events_partitioned FOR VALUES FROM (%L) TO (%L)',
        to_char(curr_start, 'YYYY_MM'), curr_start, next_start
    );
    EXECUTE format(
        'CREATE TABLE run_events_p%s PARTITION OF run_events_partitioned FOR VALUES FROM (%L) TO (%L)',
        to_char(next_start, 'YYYY_MM'), next_start, after_start
    );
END $body$;
-- +goose StatementEnd

-- 4. 分区本地索引（自动继承到每个分区）
CREATE INDEX ix_run_events_part_run_seq ON run_events_partitioned (run_id, seq);
CREATE INDEX ix_run_events_part_type ON run_events_partitioned (type);
CREATE INDEX ix_run_events_part_ts ON run_events_partitioned (ts);

-- 5. 从旧表迁移数据
INSERT INTO run_events_partitioned (event_id, run_id, seq, ts, type, data_json, tool_name, error_class)
SELECT event_id, run_id, seq, ts, type, data_json, tool_name, error_class
FROM run_events;

-- 6. 删除旧表，重命名分区表
DROP TABLE run_events;
ALTER TABLE run_events_partitioned RENAME TO run_events;

-- 7. 重命名约束和索引保持命名一致
ALTER TABLE run_events RENAME CONSTRAINT pk_run_events_part TO pk_run_events;
ALTER TABLE run_events RENAME CONSTRAINT uq_run_events_part_run_id_seq TO uq_run_events_run_id_seq;
ALTER INDEX ix_run_events_part_run_seq RENAME TO ix_run_events_run_seq;
ALTER INDEX ix_run_events_part_type RENAME TO ix_run_events_type;
ALTER INDEX ix_run_events_part_ts RENAME TO ix_run_events_ts;


-- === 00046_runs_add_status_running_index.sql ===
-- 加速 stale run reaper 的 WHERE status='running' 扫描（R73）
CREATE INDEX CONCURRENTLY ix_runs_status_running_activity
    ON runs (COALESCE(status_updated_at, created_at))
    WHERE status = 'running';


-- === 00047_notification_broadcasts_soft_delete.sql ===

ALTER TABLE notification_broadcasts ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_notification_broadcasts_active ON notification_broadcasts(created_at DESC) WHERE deleted_at IS NULL;


-- === 00048_usage_records_cache_columns.sql ===

ALTER TABLE usage_records
    ADD COLUMN cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN cached_tokens         BIGINT NOT NULL DEFAULT 0;


-- === 00049_llm_routes_cache_pricing.sql ===

ALTER TABLE llm_routes
    ADD COLUMN cost_per_1k_cache_write DOUBLE PRECISION,
    ADD COLUMN cost_per_1k_cache_read  DOUBLE PRECISION;


-- === 00050_agent_configs_cache_control.sql ===

ALTER TABLE agent_configs
    ADD COLUMN prompt_cache_control TEXT NOT NULL DEFAULT 'none';


-- === 00051_llm_credentials_advanced_json.sql ===

ALTER TABLE llm_credentials ADD COLUMN advanced_json JSONB NOT NULL DEFAULT '{}';


-- === 00052_agent_configs_scope.sql ===

ALTER TABLE agent_configs
    ADD COLUMN scope TEXT NOT NULL DEFAULT 'org';

ALTER TABLE agent_configs
    ALTER COLUMN org_id DROP NOT NULL;

-- 现有数据全部是 org 级，不需要回填


-- === 00053_orgs_add_type.sql ===

ALTER TABLE orgs
    ADD COLUMN type TEXT NOT NULL DEFAULT 'personal'
    CHECK (type IN ('personal', 'workspace'));


-- === 00054_email_verification_tokens.sql ===
CREATE TABLE email_verification_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_email_verification_tokens_user_id ON email_verification_tokens (user_id);
CREATE INDEX idx_email_verification_tokens_expires_pending ON email_verification_tokens (expires_at) WHERE used_at IS NULL;


-- === 00055_email_otp_tokens.sql ===
CREATE TABLE email_otp_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_email_otp_tokens_user_id ON email_otp_tokens (user_id);
CREATE INDEX idx_email_otp_tokens_expires_pending ON email_otp_tokens (expires_at) WHERE used_at IS NULL;


-- === 00056_skills_add_executor_fields.sql ===
ALTER TABLE skills
    ADD COLUMN executor_type TEXT NOT NULL DEFAULT 'agent.simple'
        CHECK (executor_type ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
    ADD COLUMN executor_config_json JSONB NOT NULL DEFAULT '{}';


-- === 00057_skills_add_preferred_route_id.sql ===
ALTER TABLE skills
    ADD COLUMN preferred_route_id TEXT;


-- === 00058_skills_rename_preferred_route_id.sql ===
ALTER TABLE skills
    RENAME COLUMN preferred_route_id TO preferred_credential;


-- === 00059_threads_add_private.sql ===
ALTER TABLE threads
    ADD COLUMN is_private BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN expires_at TIMESTAMPTZ;

CREATE INDEX ix_threads_private_expires ON threads(expires_at) WHERE is_private = TRUE;


-- === 00060_seed_tier_skills.sql ===

INSERT INTO skills (org_id, skill_key, version, display_name, description, prompt_md, tool_allowlist, budgets_json, is_active, executor_type, executor_config_json)
VALUES
(
    NULL,
    'lite',
    '1',
    'Lite',
    '基础对话模式，适合简单问答和轻量任务。',
    E'你是一个通用 AI 助手，运行在 Lite 模式下。\n\n保持回复简洁、准确。优先给出直接答案，避免不必要的展开。',
    '{}',
    '{"max_iterations": 5, "max_output_tokens": 2048, "temperature": 0.3}',
    TRUE,
    'agent.simple',
    '{}'
),
(
    NULL,
    'pro',
    '1',
    'Pro',
    '标准工作模式，支持工具调用和多轮推理。',
    E'你是一个通用 AI 助手，运行在 Pro 模式下。\n\n你可以使用工具完成复杂任务。根据用户需求进行多步推理，必要时主动调用工具获取信息或执行操作。保持回复结构化、有深度。',
    '{}',
    '{"max_iterations": 10, "max_output_tokens": 4096}',
    TRUE,
    'agent.simple',
    '{}'
),
(
    NULL,
    'ultra',
    '1',
    'Ultra',
    '高级模式，适合高复杂度任务、长文本生成和深度分析。',
    E'你是一个通用 AI 助手，运行在 Ultra 模式下。\n\n你拥有最高的推理深度和输出能力。面对复杂问题时，进行系统性分析，提供全面、深入的回答。可以处理长文本生成、多步骤推理和跨领域综合分析。必要时主动使用工具。',
    '{}',
    '{"max_iterations": 20, "max_output_tokens": 8192, "temperature": 0.7}',
    TRUE,
    'agent.simple',
    '{}'
),
(
    NULL,
    'auto',
    '1',
    'Auto',
    '自动路由模式，根据任务复杂度分配到 Pro 或 Ultra。',
    '此 skill 使用 classify_route executor，prompt 由 executor_config 内联定义。',
    '{}',
    '{"max_iterations": 1, "max_output_tokens": 8192}',
    TRUE,
    'task.classify_route',
    '{
        "classify_prompt": "分析以下用户消息的任务复杂度。\n如果任务是简单问答、翻译、摘要、格式转换等低复杂度任务，回复 \"pro\"。\n如果任务涉及深度分析、多步推理、代码架构设计、长文本创作等高复杂度任务，回复 \"ultra\"。\n只回复 \"pro\" 或 \"ultra\"，不要输出其他内容。",
        "default_route": "pro",
        "routes": {
            "pro": {
                "prompt_override": "你是一个通用 AI 助手，运行在 Pro 模式下。\n你可以使用工具完成复杂任务。根据用户需求进行多步推理，必要时主动调用工具获取信息或执行操作。保持回复结构化、有深度。"
            },
            "ultra": {
                "prompt_override": "你是一个通用 AI 助手，运行在 Ultra 模式下。\n你拥有最高的推理深度和输出能力。面对复杂问题时，进行系统性分析，提供全面、深入的回答。可以处理长文本生成、多步骤推理和跨领域综合分析。必要时主动使用工具。"
            }
        }
    }'
);


-- === 00061_skills_add_agent_config_name.sql ===
ALTER TABLE skills ADD COLUMN agent_config_name TEXT;


-- === 00062_users_rename_display_name_to_username.sql ===
ALTER TABLE users RENAME COLUMN display_name TO username;


-- === 00063_runs_add_parent_run_id_index.sql ===
CREATE INDEX idx_runs_parent_run_id ON runs(parent_run_id) WHERE parent_run_id IS NOT NULL;


-- === 00064_thread_stars.sql ===
CREATE TABLE thread_stars (
    user_id    UUID        NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
    thread_id  UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, thread_id)
);

CREATE INDEX thread_stars_user_id_idx ON thread_stars (user_id);


-- === 00065_asr_credentials_scope.sql ===

ALTER TABLE asr_credentials
    ADD COLUMN scope TEXT NOT NULL DEFAULT 'org';

ALTER TABLE asr_credentials
    ALTER COLUMN org_id DROP NOT NULL;

-- 替换表级 UNIQUE 约束为 scope 感知的部分索引
ALTER TABLE asr_credentials
    DROP CONSTRAINT asr_credentials_org_id_name_key;

CREATE UNIQUE INDEX asr_credentials_org_name_idx
    ON asr_credentials (org_id, name)
    WHERE scope = 'org';

CREATE UNIQUE INDEX asr_credentials_platform_name_idx
    ON asr_credentials (name)
    WHERE scope = 'platform';

-- 替换 default 唯一索引
DROP INDEX asr_credentials_org_default_idx;

CREATE UNIQUE INDEX asr_credentials_org_default_idx
    ON asr_credentials (org_id)
    WHERE scope = 'org' AND is_default = true AND revoked_at IS NULL;

-- platform 全局最多 1 个 default
CREATE UNIQUE INDEX asr_credentials_platform_default_idx
    ON asr_credentials (is_default)
    WHERE scope = 'platform' AND is_default = true AND revoked_at IS NULL;


-- === 00066_thread_shares.sql ===
CREATE TABLE thread_shares (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id              UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    token                  VARCHAR(32) NOT NULL,
    access_type            VARCHAR(16) NOT NULL DEFAULT 'public',
    password_hash          TEXT,
    snapshot_message_count INT         NOT NULL,
    created_by_user_id     UUID        NOT NULL REFERENCES users(id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_thread_shares_token     ON thread_shares(token);
CREATE UNIQUE INDEX idx_thread_shares_thread_id ON thread_shares(thread_id);


-- === 00067_skills_add_tool_denylist.sql ===
ALTER TABLE skills ADD COLUMN tool_denylist TEXT[] NOT NULL DEFAULT '{}';


-- === 00068_agent_configs_reasoning_mode.sql ===

ALTER TABLE agent_configs
    ADD COLUMN reasoning_mode TEXT NOT NULL DEFAULT 'auto';


-- === 00069_threads_add_fork_fields.sql ===
ALTER TABLE threads
    ADD COLUMN parent_thread_id UUID REFERENCES threads(id) ON DELETE SET NULL,
    ADD COLUMN branched_from_message_id UUID REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX idx_threads_parent_thread_id ON threads(parent_thread_id) WHERE parent_thread_id IS NOT NULL;


-- === 00070_threads_add_title_locked.sql ===
ALTER TABLE threads ADD COLUMN title_locked boolean NOT NULL DEFAULT false;


-- === 00071_user_memory_snapshots.sql ===
CREATE TABLE user_memory_snapshots (
    org_id       UUID NOT NULL,
    user_id      UUID NOT NULL,
    agent_id     TEXT NOT NULL DEFAULT 'default',
    memory_block TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id, agent_id)
);


-- === 00072_memory_snapshots_add_hits_json.sql ===
ALTER TABLE user_memory_snapshots ADD COLUMN hits_json JSONB;


-- === 00073_thread_reports.sql ===
CREATE TABLE thread_reports (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id   UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    reporter_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    categories  TEXT[]      NOT NULL,
    feedback    TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_thread_reports_thread_id ON thread_reports(thread_id);
CREATE INDEX idx_thread_reports_created_at ON thread_reports(created_at DESC);


-- === 00074_thread_reports_thread_id_nullable.sql ===
ALTER TABLE thread_reports ALTER COLUMN thread_id DROP NOT NULL;


-- === 00075_rename_skills_to_personas.sql ===

-- rename skills table to personas
ALTER TABLE skills RENAME TO personas;
ALTER TABLE personas RENAME COLUMN skill_key TO persona_key;

-- update constraints
ALTER TABLE personas RENAME CONSTRAINT uq_skills_org_key_version TO uq_personas_org_key_version;
ALTER TABLE personas RENAME CONSTRAINT chk_skills_key_format TO chk_personas_key_format;
ALTER TABLE personas RENAME CONSTRAINT chk_skills_version_format TO chk_personas_version_format;

-- update check constraint expressions (persona_key instead of skill_key)
ALTER TABLE personas DROP CONSTRAINT chk_personas_key_format;
ALTER TABLE personas ADD CONSTRAINT chk_personas_key_format CHECK (persona_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$');

-- rename skill_id columns in related tables
ALTER TABLE runs RENAME COLUMN skill_id TO persona_id;
ALTER TABLE agent_configs RENAME COLUMN skill_id TO persona_id;

-- update RBAC permissions
UPDATE rbac_roles SET permissions = array_replace(permissions, 'data.skills.read', 'data.personas.read');


-- === 00076_create_org_settings.sql ===

CREATE TABLE org_settings (
    org_id     uuid        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    key        text        NOT NULL,
    value      text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, key)
);

CREATE INDEX ix_org_settings_key ON org_settings(key);


-- === 00077_create_tool_provider_configs.sql ===
CREATE TABLE tool_provider_configs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    group_name    text NOT NULL,
    provider_name text NOT NULL,
    is_active     boolean NOT NULL DEFAULT false,
    secret_id     uuid REFERENCES secrets(id) ON DELETE SET NULL,
    key_prefix    text,
    base_url      text,
    config_json   jsonb NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE(org_id, provider_name)
);

CREATE INDEX ix_tool_provider_configs_org_group_active
    ON tool_provider_configs (org_id, group_name)
    WHERE is_active = true;


-- === 00078_tool_provider_active_unique.sql ===
-- 同 org + group 只允许一个 active，先清理历史脏数据再加 UNIQUE partial index。
WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY org_id, group_name
            ORDER BY updated_at DESC, created_at DESC, id DESC
        ) AS rn
    FROM tool_provider_configs
    WHERE is_active = TRUE
)
UPDATE tool_provider_configs c
SET is_active = FALSE,
    updated_at = now()
FROM ranked r
WHERE c.id = r.id
  AND r.rn > 1;

DROP INDEX IF EXISTS ix_tool_provider_configs_org_group_active;
CREATE UNIQUE INDEX ix_tool_provider_configs_org_group_active
    ON tool_provider_configs (org_id, group_name)
    WHERE is_active = true;


-- === 00079_secrets_scope.sql ===

ALTER TABLE secrets
    ADD COLUMN scope TEXT NOT NULL DEFAULT 'org';

ALTER TABLE secrets
    ALTER COLUMN org_id DROP NOT NULL;

-- 替换表级 UNIQUE 约束为 scope 感知的部分索引
ALTER TABLE secrets
    DROP CONSTRAINT uq_secrets_org_name;

CREATE UNIQUE INDEX secrets_org_name_idx
    ON secrets (org_id, name)
    WHERE scope = 'org';

CREATE UNIQUE INDEX secrets_platform_name_idx
    ON secrets (name)
    WHERE scope = 'platform';


-- === 00080_tool_provider_configs_scope.sql ===

ALTER TABLE tool_provider_configs
    ADD COLUMN scope TEXT NOT NULL DEFAULT 'org';

ALTER TABLE tool_provider_configs
    ALTER COLUMN org_id DROP NOT NULL;

-- 替换表级 UNIQUE 约束为 scope 感知的部分索引
ALTER TABLE tool_provider_configs
    DROP CONSTRAINT tool_provider_configs_org_id_provider_name_key;

CREATE UNIQUE INDEX tool_provider_configs_org_provider_idx
    ON tool_provider_configs (org_id, provider_name)
    WHERE scope = 'org';

CREATE UNIQUE INDEX tool_provider_configs_platform_provider_idx
    ON tool_provider_configs (provider_name)
    WHERE scope = 'platform';

-- 同 scope+group 只允许一个 active
DROP INDEX IF EXISTS ix_tool_provider_configs_org_group_active;

CREATE UNIQUE INDEX ix_tool_provider_configs_org_group_active
    ON tool_provider_configs (org_id, group_name)
    WHERE scope = 'org' AND is_active = true;

CREATE UNIQUE INDEX ix_tool_provider_configs_platform_group_active
    ON tool_provider_configs (group_name)
    WHERE scope = 'platform' AND is_active = true;

-- 迁移：若已有 org 级 active，选最新的作为 platform 默认
WITH ranked AS (
    SELECT
        group_name,
        provider_name,
        secret_id,
        key_prefix,
        base_url,
        config_json,
        ROW_NUMBER() OVER (
            PARTITION BY group_name
            ORDER BY updated_at DESC, created_at DESC, id DESC
        ) AS rn
    FROM tool_provider_configs
    WHERE scope = 'org' AND is_active = true
)
INSERT INTO tool_provider_configs (
    org_id,
    scope,
    group_name,
    provider_name,
    is_active,
    secret_id,
    key_prefix,
    base_url,
    config_json,
    created_at,
    updated_at
)
SELECT
    NULL,
    'platform',
    r.group_name,
    r.provider_name,
    TRUE,
    r.secret_id,
    r.key_prefix,
    r.base_url,
    r.config_json,
    now(),
    now()
FROM ranked r
WHERE r.rn = 1
  AND NOT EXISTS (
      SELECT 1
      FROM tool_provider_configs p
      WHERE p.scope = 'platform' AND p.group_name = r.group_name
  );

-- 迁移：将被 platform 引用的 tool_provider secret 提升为 platform scope
UPDATE secrets s
SET scope = 'platform',
    org_id = NULL,
    updated_at = now()
WHERE s.id IN (
    SELECT secret_id
    FROM tool_provider_configs
    WHERE scope = 'platform' AND secret_id IS NOT NULL
)
  AND s.scope = 'org'
  AND s.name LIKE 'tool_provider:%';


-- === 00081_credit_transactions_metadata.sql ===

ALTER TABLE credit_transactions ADD COLUMN metadata JSONB;


-- === 00082_usage_records_type.sql ===

ALTER TABLE usage_records ADD COLUMN usage_type TEXT NOT NULL DEFAULT 'llm';

ALTER TABLE usage_records DROP CONSTRAINT usage_records_run_id_key;
ALTER TABLE usage_records ADD CONSTRAINT usage_records_run_id_usage_type_key UNIQUE (run_id, usage_type);


-- === 00083_thread_shares_multi_url.sql ===
DROP INDEX IF EXISTS idx_thread_shares_thread_id;
ALTER TABLE thread_shares ADD COLUMN live_update BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE thread_shares ADD COLUMN snapshot_turn_count INT NOT NULL DEFAULT 0;
CREATE INDEX idx_thread_shares_thread_id ON thread_shares(thread_id);


-- === 00084_thread_shares_plaintext_password.sql ===
ALTER TABLE thread_shares RENAME COLUMN password_hash TO password;


-- === 00085_personas_remove_global_rows.sql ===
DELETE FROM personas WHERE org_id IS NULL;


-- === 00086_reasoning_iterations_budget_split.sql ===

INSERT INTO platform_settings (key, value, updated_at)
SELECT 'limit.agent_reasoning_iterations', value, updated_at
FROM platform_settings
WHERE key = 'limit.agent_max_iterations'
ON CONFLICT (key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;

DELETE FROM platform_settings WHERE key = 'limit.agent_max_iterations';

INSERT INTO org_settings (org_id, key, value, updated_at)
SELECT org_id, 'limit.agent_reasoning_iterations', value, updated_at
FROM org_settings
WHERE key = 'limit.agent_max_iterations'
ON CONFLICT (org_id, key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;

DELETE FROM org_settings WHERE key = 'limit.agent_max_iterations';

UPDATE personas
SET budgets_json = CASE
    WHEN budgets_json ? 'reasoning_iterations' THEN budgets_json - 'max_iterations'
    ELSE jsonb_set(budgets_json - 'max_iterations', '{reasoning_iterations}', budgets_json -> 'max_iterations')
END
WHERE budgets_json ? 'max_iterations';


-- === 00087_tool_description_overrides.sql ===
CREATE TABLE IF NOT EXISTS tool_description_overrides (
    org_id      uuid        NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    scope       text        NOT NULL DEFAULT 'platform',
    tool_name   text        NOT NULL,
    description text        NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, scope, tool_name)
);


-- === 00088_seed_sandbox_memory_providers.sql ===

-- Seed sandbox provider from platform_settings if configured
INSERT INTO tool_provider_configs (
    org_id, scope, group_name, provider_name, is_active, base_url, config_json
)
SELECT
    NULL,
    'platform',
    'sandbox',
    CASE
        WHEN value LIKE '%firecracker%' THEN 'sandbox.firecracker'
        ELSE 'sandbox.docker'
    END,
    true,
    value,
    '{}'::jsonb
FROM platform_settings
WHERE key = 'sandbox.base_url' AND TRIM(value) != ''
ON CONFLICT DO NOTHING;

-- Seed memory (openviking) provider from platform_settings if configured
-- api_key cannot be migrated here (requires encryption); configure via Console UI
INSERT INTO tool_provider_configs (
    org_id, scope, group_name, provider_name, is_active, base_url, config_json
)
SELECT
    NULL,
    'platform',
    'memory',
    'memory.openviking',
    true,
    value,
    '{}'::jsonb
FROM platform_settings
WHERE key = 'openviking.base_url' AND TRIM(value) != ''
ON CONFLICT DO NOTHING;


-- === 00089_webhook_secret_id.sql ===
ALTER TABLE webhook_endpoints
    ADD COLUMN secret_id UUID REFERENCES secrets(id) ON DELETE SET NULL;

ALTER TABLE webhook_endpoints
    ALTER COLUMN signing_secret DROP NOT NULL;

CREATE INDEX idx_webhook_endpoints_secret_id ON webhook_endpoints(secret_id);


-- === 00091_llm_routes_provider_models.sql ===

ALTER TABLE llm_routes
    ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}'::text[];

WITH ranked_duplicates AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY credential_id, lower(model)
               ORDER BY priority DESC, is_default DESC, created_at ASC, id ASC
           ) AS row_num
    FROM llm_routes
)
DELETE FROM llm_routes r
USING ranked_duplicates d
WHERE r.id = d.id
  AND d.row_num > 1;

WITH ranked_defaults AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY credential_id
               ORDER BY priority DESC, created_at ASC, id ASC
           ) AS row_num
    FROM llm_routes
    WHERE is_default = TRUE
)
UPDATE llm_routes r
SET is_default = FALSE
FROM ranked_defaults d
WHERE r.id = d.id
  AND d.row_num > 1;

CREATE UNIQUE INDEX ux_llm_routes_credential_model_lower
    ON llm_routes (credential_id, lower(model));

CREATE UNIQUE INDEX ux_llm_routes_credential_default
    ON llm_routes (credential_id)
    WHERE is_default = TRUE;


-- === 00092_persona_absorb_agent_config.sql ===

ALTER TABLE personas
    ADD COLUMN model TEXT,
    ADD COLUMN reasoning_mode TEXT NOT NULL DEFAULT 'auto',
    ADD COLUMN prompt_cache_control TEXT NOT NULL DEFAULT 'none';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arkloop_merge_persona_budgets(
    base JSONB,
    cfg_temperature DOUBLE PRECISION,
    cfg_max_output_tokens INT,
    cfg_top_p DOUBLE PRECISION
) RETURNS JSONB
LANGUAGE plpgsql
AS $$
DECLARE
    result JSONB := COALESCE(base, '{}'::jsonb);
    existing_max INT;
BEGIN
    IF jsonb_typeof(result) IS DISTINCT FROM 'object' THEN
        result := '{}'::jsonb;
    END IF;

    IF cfg_temperature IS NOT NULL AND NOT (result ? 'temperature') THEN
        result := result || jsonb_build_object('temperature', cfg_temperature);
    END IF;

    IF cfg_top_p IS NOT NULL AND NOT (result ? 'top_p') THEN
        result := result || jsonb_build_object('top_p', cfg_top_p);
    END IF;

    IF cfg_max_output_tokens IS NOT NULL THEN
        IF result ? 'max_output_tokens' THEN
            BEGIN
                existing_max := floor((result ->> 'max_output_tokens')::numeric)::INT;
            EXCEPTION WHEN OTHERS THEN
                existing_max := NULL;
            END;

            IF existing_max IS NULL THEN
                result := result || jsonb_build_object('max_output_tokens', cfg_max_output_tokens);
            ELSE
                result := result || jsonb_build_object('max_output_tokens', LEAST(existing_max, cfg_max_output_tokens));
            END IF;
        ELSE
            result := result || jsonb_build_object('max_output_tokens', cfg_max_output_tokens);
        END IF;
    END IF;

    RETURN result;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arkloop_intersect_text_arrays(left_arr TEXT[], right_arr TEXT[])
RETURNS TEXT[]
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT COALESCE(array_agg(value ORDER BY value), '{}'::TEXT[])
    FROM (
        SELECT DISTINCT l.value
        FROM unnest(COALESCE(left_arr, '{}'::TEXT[])) AS l(value)
        INNER JOIN unnest(COALESCE(right_arr, '{}'::TEXT[])) AS r(value)
            ON l.value = r.value
    ) AS matched(value);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arkloop_union_text_arrays(left_arr TEXT[], right_arr TEXT[])
RETURNS TEXT[]
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT COALESCE(array_agg(value ORDER BY value), '{}'::TEXT[])
    FROM (
        SELECT DISTINCT value
        FROM unnest(COALESCE(left_arr, '{}'::TEXT[]) || COALESCE(right_arr, '{}'::TEXT[])) AS merged(value)
    ) AS deduped;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arkloop_merge_prompt(prefix TEXT, prompt TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE
        WHEN NULLIF(BTRIM(COALESCE(prefix, '')), '') IS NOT NULL
         AND NULLIF(BTRIM(COALESCE(prompt, '')), '') IS NOT NULL
            THEN BTRIM(prefix) || E'\n\n' || BTRIM(prompt)
        WHEN NULLIF(BTRIM(COALESCE(prefix, '')), '') IS NOT NULL
            THEN BTRIM(prefix)
        ELSE BTRIM(COALESCE(prompt, ''))
    END;
$$;
-- +goose StatementEnd

WITH explicit_match AS (
    SELECT
        p.id AS persona_id,
        ac.id,
        ac.scope,
        ac.org_id,
        ac.model,
        ac.reasoning_mode,
        ac.prompt_cache_control,
        ac.temperature,
        ac.max_output_tokens,
        ac.top_p,
        ac.tool_policy,
        ac.tool_allowlist,
        ac.tool_denylist,
        ac.system_prompt_override,
        ac.system_prompt_template_id,
        ac.created_at,
        1 AS match_priority,
        CASE WHEN ac.scope = 'org' AND ac.org_id = p.org_id THEN 0 ELSE 1 END AS scope_priority
    FROM personas p
    JOIN agent_configs ac
      ON p.agent_config_name IS NOT NULL
     AND LOWER(ac.name) = LOWER(p.agent_config_name)
     AND ((ac.scope = 'org' AND ac.org_id = p.org_id) OR ac.scope = 'platform')
),
persona_match AS (
    SELECT
        p.id AS persona_id,
        ac.id,
        ac.scope,
        ac.org_id,
        ac.model,
        ac.reasoning_mode,
        ac.prompt_cache_control,
        ac.temperature,
        ac.max_output_tokens,
        ac.top_p,
        ac.tool_policy,
        ac.tool_allowlist,
        ac.tool_denylist,
        ac.system_prompt_override,
        ac.system_prompt_template_id,
        ac.created_at,
        2 AS match_priority,
        CASE WHEN ac.scope = 'org' AND ac.org_id = p.org_id THEN 0 ELSE 1 END AS scope_priority
    FROM personas p
    JOIN agent_configs ac
      ON ac.persona_id = p.id
),
ranked_match AS (
    SELECT *, ROW_NUMBER() OVER (
        PARTITION BY persona_id
        ORDER BY match_priority ASC, scope_priority ASC, created_at DESC, id DESC
    ) AS row_num
    FROM (
        SELECT * FROM explicit_match
        UNION ALL
        SELECT * FROM persona_match
    ) AS merged
),
selected_match AS (
    SELECT
        rm.persona_id,
        rm.model,
        rm.reasoning_mode,
        rm.prompt_cache_control,
        rm.temperature,
        rm.max_output_tokens,
        rm.top_p,
        rm.tool_policy,
        rm.tool_allowlist,
        rm.tool_denylist,
        COALESCE(NULLIF(BTRIM(rm.system_prompt_override), ''), NULLIF(BTRIM(pt.content), '')) AS resolved_prompt_prefix
    FROM ranked_match rm
    LEFT JOIN prompt_templates pt
      ON pt.id = rm.system_prompt_template_id
    WHERE rm.row_num = 1
)
UPDATE personas p
SET model = sm.model,
    reasoning_mode = COALESCE(NULLIF(BTRIM(sm.reasoning_mode), ''), 'auto'),
    prompt_cache_control = COALESCE(NULLIF(BTRIM(sm.prompt_cache_control), ''), 'none'),
    prompt_md = arkloop_merge_prompt(sm.resolved_prompt_prefix, p.prompt_md),
    budgets_json = arkloop_merge_persona_budgets(p.budgets_json, sm.temperature, sm.max_output_tokens, sm.top_p),
    tool_allowlist = CASE
        WHEN sm.tool_policy = 'allowlist' THEN
            CASE
                WHEN COALESCE(array_length(p.tool_allowlist, 1), 0) > 0
                    THEN arkloop_intersect_text_arrays(p.tool_allowlist, sm.tool_allowlist)
                ELSE COALESCE(sm.tool_allowlist, '{}'::TEXT[])
            END
        ELSE p.tool_allowlist
    END,
    tool_denylist = CASE
        WHEN sm.tool_policy = 'denylist'
            THEN arkloop_union_text_arrays(p.tool_denylist, sm.tool_denylist)
        ELSE p.tool_denylist
    END
FROM selected_match sm
WHERE sm.persona_id = p.id;

DROP FUNCTION IF EXISTS arkloop_merge_persona_budgets(JSONB, DOUBLE PRECISION, INT, DOUBLE PRECISION);
DROP FUNCTION IF EXISTS arkloop_intersect_text_arrays(TEXT[], TEXT[]);
DROP FUNCTION IF EXISTS arkloop_union_text_arrays(TEXT[], TEXT[]);
DROP FUNCTION IF EXISTS arkloop_merge_prompt(TEXT, TEXT);

UPDATE rbac_roles
SET permissions = array_remove(array_remove(permissions, 'data.agent_configs.read'), 'data.agent_configs.manage')
WHERE permissions && ARRAY['data.agent_configs.read', 'data.agent_configs.manage'];

ALTER TABLE threads DROP COLUMN IF EXISTS agent_config_id;
ALTER TABLE personas DROP COLUMN IF EXISTS agent_config_name;

DROP TABLE IF EXISTS agent_configs;
DROP TABLE IF EXISTS prompt_templates;


-- === 00093_clear_tool_description_overrides.sql ===
TRUNCATE TABLE tool_description_overrides;


-- === 00094_run_environment_bindings.sql ===

ALTER TABLE runs
    ADD COLUMN profile_ref TEXT,
    ADD COLUMN workspace_ref TEXT;

CREATE TABLE default_workspace_bindings (
    profile_ref       TEXT        NOT NULL,
    owner_user_id     UUID,
    org_id            UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    binding_scope     TEXT        NOT NULL CHECK (binding_scope IN ('project', 'thread')),
    binding_target_id UUID        NOT NULL,
    workspace_ref     TEXT        NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, profile_ref, binding_scope, binding_target_id)
);

CREATE INDEX idx_default_workspace_bindings_owner_user_id
    ON default_workspace_bindings (owner_user_id)
    WHERE owner_user_id IS NOT NULL;

CREATE UNIQUE INDEX idx_default_workspace_bindings_workspace_ref
    ON default_workspace_bindings (workspace_ref);


-- === 00095_shell_session_refs.sql ===

CREATE TABLE shell_sessions (
    session_ref           TEXT        PRIMARY KEY,
    org_id                UUID        NOT NULL,
    profile_ref           TEXT        NOT NULL,
    workspace_ref         TEXT        NOT NULL,
    project_id            UUID        NULL,
    thread_id             UUID        NULL,
    run_id                UUID        NULL,
    share_scope           TEXT        NOT NULL,
    state                 TEXT        NOT NULL,
    live_session_id       TEXT        NULL,
    latest_checkpoint_rev TEXT        NULL,
    last_used_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_shell_sessions_org_thread
    ON shell_sessions (org_id, thread_id);

CREATE INDEX idx_shell_sessions_org_workspace
    ON shell_sessions (org_id, workspace_ref);

CREATE INDEX idx_shell_sessions_org_run
    ON shell_sessions (org_id, run_id);

CREATE TABLE default_shell_session_bindings (
    org_id         UUID        NOT NULL,
    profile_ref    TEXT        NOT NULL,
    binding_scope  TEXT        NOT NULL,
    binding_target TEXT        NOT NULL,
    session_ref    TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, profile_ref, binding_scope, binding_target)
);

CREATE UNIQUE INDEX idx_default_shell_session_bindings_session_ref
    ON default_shell_session_bindings (session_ref);


-- === 00096_smtp_providers.sql ===

CREATE TABLE smtp_providers (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    from_addr   TEXT        NOT NULL,
    smtp_host   TEXT        NOT NULL,
    smtp_port   INTEGER     NOT NULL DEFAULT 587,
    smtp_user   TEXT        NOT NULL DEFAULT '',
    smtp_pass   TEXT        NOT NULL DEFAULT '',
    tls_mode    TEXT        NOT NULL DEFAULT 'starttls'
                            CHECK (tls_mode IN ('starttls', 'tls', 'none')),
    is_default  BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 从已有 email.* platform_settings 迁移
INSERT INTO smtp_providers (name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default)
SELECT
    'Default',
    COALESCE(ps_from.value, ''),
    COALESCE(ps_host.value, ''),
    COALESCE(NULLIF(ps_port.value, '')::INTEGER, 587),
    COALESCE(ps_user.value, ''),
    COALESCE(ps_pass.value, ''),
    COALESCE(NULLIF(ps_tls.value, ''), 'starttls'),
    true
FROM platform_settings ps_from
LEFT JOIN platform_settings ps_host ON ps_host.key = 'email.smtp_host'
LEFT JOIN platform_settings ps_port ON ps_port.key = 'email.smtp_port'
LEFT JOIN platform_settings ps_user ON ps_user.key = 'email.smtp_user'
LEFT JOIN platform_settings ps_pass ON ps_pass.key = 'email.smtp_pass'
LEFT JOIN platform_settings ps_tls  ON ps_tls.key  = 'email.smtp_tls_mode'
WHERE ps_from.key = 'email.from' AND TRIM(ps_from.value) != '';


-- === 00097_repair_shell_session_refs.sql ===

CREATE TABLE IF NOT EXISTS shell_sessions (
    session_ref           TEXT        PRIMARY KEY,
    org_id                UUID        NOT NULL,
    profile_ref           TEXT        NOT NULL,
    workspace_ref         TEXT        NOT NULL,
    project_id            UUID        NULL,
    thread_id             UUID        NULL,
    run_id                UUID        NULL,
    share_scope           TEXT        NOT NULL,
    state                 TEXT        NOT NULL,
    live_session_id       TEXT        NULL,
    latest_checkpoint_rev TEXT        NULL,
    last_used_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_thread
    ON shell_sessions (org_id, thread_id);

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_workspace
    ON shell_sessions (org_id, workspace_ref);

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_run
    ON shell_sessions (org_id, run_id);

CREATE TABLE IF NOT EXISTS default_shell_session_bindings (
    org_id         UUID        NOT NULL,
    profile_ref    TEXT        NOT NULL,
    binding_scope  TEXT        NOT NULL,
    binding_target TEXT        NOT NULL,
    session_ref    TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, profile_ref, binding_scope, binding_target)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_default_shell_session_bindings_session_ref
    ON default_shell_session_bindings (session_ref);


-- === 00098_tool_description_overrides_disabled.sql ===
ALTER TABLE tool_description_overrides
    ADD COLUMN IF NOT EXISTS is_disabled boolean NOT NULL DEFAULT FALSE;


-- === 00099_flush_registries.sql ===

CREATE TABLE profile_registries (
    profile_ref             TEXT        PRIMARY KEY,
    org_id                  UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    latest_manifest_rev     TEXT        NULL,
    flush_state             TEXT        NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER     NOT NULL DEFAULT 0,
    last_flush_failed_at    TIMESTAMPTZ NULL,
    last_flush_succeeded_at TIMESTAMPTZ NULL,
    metadata_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_profile_registries_org_id
    ON profile_registries (org_id);

CREATE TABLE workspace_registries (
    workspace_ref           TEXT        PRIMARY KEY,
    org_id                  UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    latest_manifest_rev     TEXT        NULL,
    flush_state             TEXT        NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER     NOT NULL DEFAULT 0,
    last_flush_failed_at    TIMESTAMPTZ NULL,
    last_flush_succeeded_at TIMESTAMPTZ NULL,
    metadata_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workspace_registries_org_id
    ON workspace_registries (org_id);

ALTER TABLE shell_sessions
    ADD COLUMN IF NOT EXISTS latest_restore_rev TEXT NULL;


-- === 00100_drop_legacy_shell_checkpoint.sql ===

ALTER TABLE shell_sessions
    DROP COLUMN IF EXISTS latest_checkpoint_rev;


-- === 00101_flush_registry_leases.sql ===

ALTER TABLE profile_registries
    ADD COLUMN IF NOT EXISTS lease_holder_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ NULL;

ALTER TABLE profile_registries
    DROP CONSTRAINT IF EXISTS profile_registries_lease_consistency;

ALTER TABLE profile_registries
    ADD CONSTRAINT profile_registries_lease_consistency
        CHECK (
            (lease_holder_id IS NULL AND lease_until IS NULL)
            OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
        );

ALTER TABLE workspace_registries
    ADD COLUMN IF NOT EXISTS lease_holder_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ NULL;

ALTER TABLE workspace_registries
    DROP CONSTRAINT IF EXISTS workspace_registries_lease_consistency;

ALTER TABLE workspace_registries
    ADD CONSTRAINT workspace_registries_lease_consistency
        CHECK (
            (lease_holder_id IS NULL AND lease_until IS NULL)
            OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
        );


-- === 00102_session_registry_truth_source.sql ===

ALTER TABLE shell_sessions
    ADD COLUMN IF NOT EXISTS default_binding_key TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_profile_default_binding_updated
    ON shell_sessions (org_id, profile_ref, default_binding_key, updated_at DESC)
    WHERE default_binding_key IS NOT NULL;

ALTER TABLE profile_registries
    ADD COLUMN IF NOT EXISTS owner_user_id UUID NULL,
    ADD COLUMN IF NOT EXISTS default_workspace_ref TEXT NULL,
    ADD COLUMN IF NOT EXISTS store_key TEXT NULL,
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE workspace_registries
    ADD COLUMN IF NOT EXISTS owner_user_id UUID NULL,
    ADD COLUMN IF NOT EXISTS project_id UUID NULL,
    ADD COLUMN IF NOT EXISTS default_shell_session_ref TEXT NULL,
    ADD COLUMN IF NOT EXISTS store_key TEXT NULL,
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE shell_sessions ss
   SET default_binding_key = CASE dsb.binding_scope
       WHEN 'thread' THEN 'thread:' || dsb.binding_target
       WHEN 'workspace' THEN 'workspace:' || dsb.binding_target
       ELSE NULL
   END,
       updated_at = now()
  FROM default_shell_session_bindings dsb
 WHERE ss.org_id = dsb.org_id
   AND ss.profile_ref = dsb.profile_ref
   AND ss.session_ref = dsb.session_ref
   AND ss.default_binding_key IS NULL;

DROP INDEX IF EXISTS idx_default_shell_session_bindings_session_ref;
DROP TABLE IF EXISTS default_shell_session_bindings;


-- === 00103_browser_state_registries.sql ===

CREATE TABLE browser_state_registries (
    profile_ref             TEXT        PRIMARY KEY,
    org_id                  UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    owner_user_id           UUID        NULL,
    latest_manifest_rev     TEXT        NULL,
    lease_holder_id         TEXT        NULL,
    lease_until             TIMESTAMPTZ NULL,
    store_key               TEXT        NULL,
    flush_state             TEXT        NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER     NOT NULL DEFAULT 0,
    last_used_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_flush_failed_at    TIMESTAMPTZ NULL,
    last_flush_succeeded_at TIMESTAMPTZ NULL,
    metadata_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT browser_state_registries_lease_consistency CHECK (
        (lease_holder_id IS NULL AND lease_until IS NULL)
        OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
    )
);

CREATE INDEX idx_browser_state_registries_org_id
    ON browser_state_registries (org_id);


-- === 00104_shell_session_writer_leases.sql ===

ALTER TABLE shell_sessions
    ADD COLUMN IF NOT EXISTS lease_owner_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS lease_epoch BIGINT NOT NULL DEFAULT 0;

ALTER TABLE shell_sessions
    DROP CONSTRAINT IF EXISTS shell_sessions_lease_consistency;

ALTER TABLE shell_sessions
    ADD CONSTRAINT shell_sessions_lease_consistency CHECK (
        (lease_owner_id IS NULL AND lease_until IS NULL)
        OR (lease_owner_id IS NOT NULL AND lease_until IS NOT NULL)
    );

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_lease_until
    ON shell_sessions (org_id, lease_until)
    WHERE lease_until IS NOT NULL;


-- === 00105_shell_session_types.sql ===

ALTER TABLE shell_sessions
    ADD COLUMN IF NOT EXISTS session_type TEXT NOT NULL DEFAULT 'shell';

ALTER TABLE shell_sessions
    DROP CONSTRAINT IF EXISTS shell_sessions_session_type_check;

ALTER TABLE shell_sessions
    ADD CONSTRAINT shell_sessions_session_type_check CHECK (session_type IN ('shell', 'browser'));

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_run_type
    ON shell_sessions (org_id, run_id, session_type);

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_profile_binding_type_updated
    ON shell_sessions (org_id, profile_ref, session_type, default_binding_key, updated_at DESC)
    WHERE default_binding_key IS NOT NULL;


-- === 00106_shell_session_default_binding_uniqueness.sql ===

WITH ranked AS (
    SELECT session_ref,
           row_number() OVER (
               PARTITION BY org_id, profile_ref, session_type, default_binding_key
               ORDER BY updated_at DESC, created_at DESC, session_ref DESC
           ) AS rank_order
      FROM shell_sessions
     WHERE default_binding_key IS NOT NULL
       AND state <> 'closed'
)
UPDATE shell_sessions AS sessions
   SET default_binding_key = NULL,
       updated_at = now(),
       last_used_at = now()
  FROM ranked
 WHERE sessions.session_ref = ranked.session_ref
   AND ranked.rank_order > 1;

DROP INDEX IF EXISTS idx_shell_sessions_org_profile_binding_type_updated;

CREATE UNIQUE INDEX IF NOT EXISTS idx_shell_sessions_org_profile_binding_type_unique
    ON shell_sessions (org_id, profile_ref, session_type, default_binding_key)
    WHERE default_binding_key IS NOT NULL
      AND state <> 'closed';


-- === 00107_skill_packages.sql ===

CREATE TABLE skill_packages (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID        NOT NULL,
    skill_key        TEXT        NOT NULL,
    version          TEXT        NOT NULL,
    display_name     TEXT        NOT NULL,
    description      TEXT        NULL,
    instruction_path TEXT        NOT NULL,
    manifest_key     TEXT        NOT NULL,
    bundle_key       TEXT        NOT NULL,
    files_prefix     TEXT        NOT NULL,
    platforms        TEXT[]      NOT NULL DEFAULT '{}',
    is_active        BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_skill_packages_org_key_version UNIQUE (org_id, skill_key, version),
    CONSTRAINT chk_skill_packages_key_format CHECK (skill_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
    CONSTRAINT chk_skill_packages_version_format CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$')
);

CREATE TABLE profile_skill_installs (
    profile_ref       TEXT        NOT NULL,
    org_id            UUID        NOT NULL,
    owner_user_id     UUID        NOT NULL,
    skill_key         TEXT        NOT NULL,
    version           TEXT        NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (profile_ref, skill_key, version),
    CONSTRAINT fk_profile_skill_installs_package FOREIGN KEY (org_id, skill_key, version)
        REFERENCES skill_packages (org_id, skill_key, version)
        ON DELETE CASCADE
);

CREATE INDEX idx_profile_skill_installs_profile_ref
    ON profile_skill_installs (org_id, profile_ref);

CREATE TABLE workspace_skill_enablements (
    workspace_ref       TEXT        NOT NULL,
    org_id              UUID        NOT NULL,
    enabled_by_user_id  UUID        NOT NULL,
    skill_key           TEXT        NOT NULL,
    version             TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_ref, skill_key),
    CONSTRAINT fk_workspace_skill_enablements_package FOREIGN KEY (org_id, skill_key, version)
        REFERENCES skill_packages (org_id, skill_key, version)
        ON DELETE CASCADE
);

CREATE INDEX idx_workspace_skill_enablements_workspace_ref
    ON workspace_skill_enablements (org_id, workspace_ref);


-- === 00108_llm_routes_advanced_json.sql ===

ALTER TABLE llm_routes
    ADD COLUMN advanced_json JSONB NOT NULL DEFAULT '{}'::jsonb;


-- === 00109_browser_state_workspace_scope.sql ===

DROP INDEX IF EXISTS idx_browser_state_registries_org_id;
DROP TABLE IF EXISTS browser_state_registries;

CREATE TABLE browser_state_registries (
    workspace_ref           TEXT        PRIMARY KEY,
    org_id                  UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    owner_user_id           UUID        NULL,
    latest_manifest_rev     TEXT        NULL,
    lease_holder_id         TEXT        NULL,
    lease_until             TIMESTAMPTZ NULL,
    store_key               TEXT        NULL,
    flush_state             TEXT        NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER     NOT NULL DEFAULT 0,
    last_used_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_flush_failed_at    TIMESTAMPTZ NULL,
    last_flush_succeeded_at TIMESTAMPTZ NULL,
    metadata_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT browser_state_registries_lease_consistency CHECK (
        (lease_holder_id IS NULL AND lease_until IS NULL)
        OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
    )
);

CREATE INDEX idx_browser_state_registries_org_id
    ON browser_state_registries (org_id);


-- === 00110_llm_credentials_scope.sql ===

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


-- === 00111_projects_default_owner.sql ===

ALTER TABLE projects
    ADD COLUMN owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX idx_projects_owner_user_id
    ON projects(owner_user_id)
    WHERE owner_user_id IS NOT NULL;

CREATE UNIQUE INDEX uq_projects_default_per_owner
    ON projects(org_id, owner_user_id)
    WHERE deleted_at IS NULL AND is_default = true;

ALTER TABLE threads
    ALTER COLUMN project_id SET NOT NULL;


-- === 00112_threads_add_mode.sql ===
ALTER TABLE threads ADD COLUMN mode TEXT;

UPDATE threads
SET mode = 'chat'
WHERE mode IS NULL;

ALTER TABLE threads
    ALTER COLUMN mode SET DEFAULT 'chat',
    ALTER COLUMN mode SET NOT NULL,
    ADD CONSTRAINT chk_threads_mode CHECK (mode IN ('chat', 'claw'));


-- === 00113_skill_packages_registry_metadata.sql ===

ALTER TABLE skill_packages
    ADD COLUMN registry_provider TEXT NULL,
    ADD COLUMN registry_slug TEXT NULL,
    ADD COLUMN registry_owner_handle TEXT NULL,
    ADD COLUMN registry_version TEXT NULL,
    ADD COLUMN registry_detail_url TEXT NULL,
    ADD COLUMN registry_download_url TEXT NULL,
    ADD COLUMN registry_source_kind TEXT NULL,
    ADD COLUMN registry_source_url TEXT NULL,
    ADD COLUMN scan_status TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN scan_has_warnings BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN scan_checked_at TIMESTAMPTZ NULL,
    ADD COLUMN scan_engine TEXT NULL,
    ADD COLUMN scan_summary TEXT NULL,
    ADD COLUMN moderation_verdict TEXT NULL,
    ADD COLUMN scan_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE skill_packages
    ADD CONSTRAINT chk_skill_packages_scan_status
    CHECK (scan_status IN ('clean', 'suspicious', 'malicious', 'pending', 'error', 'unknown'));

CREATE INDEX idx_skill_packages_registry_slug
    ON skill_packages (registry_provider, registry_slug, registry_version);


-- === 00114_deorg_foundation.sql ===

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS project_settings (
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key         TEXT        NOT NULL,
    value       TEXT        NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, key)
);

CREATE INDEX IF NOT EXISTS ix_project_settings_key ON project_settings(key);

ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX IF NOT EXISTS uq_personas_project_key_version
    ON personas (project_id, persona_key, version)
    WHERE project_id IS NOT NULL;

ALTER TABLE secrets
    ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE secrets DROP CONSTRAINT IF EXISTS chk_secrets_owner_kind;
ALTER TABLE secrets
    ADD CONSTRAINT chk_secrets_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX IF NOT EXISTS secrets_user_name_idx
    ON secrets (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

ALTER TABLE llm_credentials
    ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE llm_credentials DROP CONSTRAINT IF EXISTS chk_llm_credentials_owner_kind;
ALTER TABLE llm_credentials
    ADD CONSTRAINT chk_llm_credentials_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX IF NOT EXISTS llm_credentials_user_name_idx
    ON llm_credentials (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

ALTER TABLE asr_credentials
    ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE asr_credentials DROP CONSTRAINT IF EXISTS chk_asr_credentials_owner_kind;
ALTER TABLE asr_credentials
    ADD CONSTRAINT chk_asr_credentials_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX IF NOT EXISTS asr_credentials_user_name_idx
    ON asr_credentials (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

ALTER TABLE llm_routes
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS route_key TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT;

UPDATE llm_routes
SET route_key = id::TEXT
WHERE NULLIF(BTRIM(route_key), '') IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_llm_routes_route_key
    ON llm_routes (LOWER(route_key));

CREATE INDEX IF NOT EXISTS ix_llm_routes_project_id ON llm_routes(project_id) WHERE project_id IS NOT NULL;

ALTER TABLE tool_provider_configs
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS ix_tool_provider_configs_project_group_active
    ON tool_provider_configs (project_id, group_name)
    WHERE project_id IS NOT NULL AND is_active = TRUE;

ALTER TABLE tool_description_overrides
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS ix_tool_description_overrides_project_tool
    ON tool_description_overrides (project_id, tool_name)
    WHERE project_id IS NOT NULL;

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

UPDATE runs AS r
SET project_id = t.project_id
FROM threads AS t
WHERE t.id = r.thread_id
  AND r.project_id IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION arkloop_runs_fill_project_id()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id IS NULL AND NEW.thread_id IS NOT NULL THEN
        SELECT project_id INTO NEW.project_id
        FROM threads
        WHERE id = NEW.thread_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_runs_fill_project_id ON runs;
CREATE TRIGGER trg_runs_fill_project_id
    BEFORE INSERT OR UPDATE OF thread_id, project_id ON runs
    FOR EACH ROW
    EXECUTE FUNCTION arkloop_runs_fill_project_id();

CREATE INDEX IF NOT EXISTS ix_runs_project_id_created_at_id
    ON runs (project_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL AND project_id IS NOT NULL;


-- === 00115_scope_org_to_project.sql ===
UPDATE llm_credentials SET scope = 'project' WHERE scope = 'org';
DROP INDEX IF EXISTS llm_credentials_org_name_idx;
CREATE UNIQUE INDEX IF NOT EXISTS llm_credentials_project_name_idx
    ON llm_credentials (org_id, name) WHERE scope = 'project';


-- === 00116_drop_webhook_signing_secret.sql ===

-- 将残留的 signing_secret 迁移到 secrets 表，然后删除 signing_secret 列。
-- backfillWebhookSecrets 运行时代码已在启动时处理过绝大多数行，
-- 此 migration 作为最终兜底，确保列可以安全删除。

-- 为尚未迁移的行创建 secret 记录并回填 secret_id
-- +goose StatementBegin
DO $$
DECLARE
    rec RECORD;
    new_secret_id UUID;
BEGIN
    FOR rec IN
        SELECT id, org_id, signing_secret
        FROM webhook_endpoints
        WHERE signing_secret IS NOT NULL AND secret_id IS NULL
    LOOP
        new_secret_id := gen_random_uuid();
        INSERT INTO secrets (id, org_id, name, encrypted_value, key_version)
        VALUES (new_secret_id, rec.org_id, 'webhook_endpoint:' || rec.id::text, rec.signing_secret, 1);
        UPDATE webhook_endpoints SET secret_id = new_secret_id WHERE id = rec.id;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE webhook_endpoints DROP COLUMN signing_secret;


-- === 00117_deorg_constraints.sql ===

-- ============================================================
-- Phase 1: Backfill project_id from org_id -> default project
-- ============================================================

UPDATE personas p
SET project_id = (
    SELECT pr.id FROM projects pr
    WHERE pr.org_id = p.org_id AND pr.deleted_at IS NULL
    ORDER BY pr.created_at ASC LIMIT 1
)
WHERE p.org_id IS NOT NULL AND p.project_id IS NULL;

UPDATE tool_provider_configs tpc
SET project_id = (
    SELECT pr.id FROM projects pr
    WHERE pr.org_id = tpc.org_id AND pr.deleted_at IS NULL
    ORDER BY pr.created_at ASC LIMIT 1
)
WHERE tpc.scope = 'org' AND tpc.org_id IS NOT NULL AND tpc.project_id IS NULL;

UPDATE tool_description_overrides tdo
SET project_id = (
    SELECT pr.id FROM projects pr
    WHERE pr.org_id = tdo.org_id AND pr.deleted_at IS NULL
    ORDER BY pr.created_at ASC LIMIT 1
)
WHERE tdo.scope = 'org' AND tdo.org_id != '00000000-0000-0000-0000-000000000000' AND tdo.project_id IS NULL;

-- ============================================================
-- Phase 2: Normalize scope values org -> project
-- ============================================================

UPDATE tool_provider_configs SET scope = 'project' WHERE scope = 'org';
UPDATE tool_description_overrides SET scope = 'project' WHERE scope = 'org';

-- ============================================================
-- Phase 3: Personas constraints
-- ============================================================

ALTER TABLE personas DROP CONSTRAINT IF EXISTS uq_personas_org_key_version;

CREATE UNIQUE INDEX IF NOT EXISTS uq_personas_platform_key_version
    ON personas (persona_key, version)
    WHERE project_id IS NULL;

-- ============================================================
-- Phase 4: tool_provider_configs constraints
-- ============================================================

DROP INDEX IF EXISTS tool_provider_configs_org_provider_idx;
DROP INDEX IF EXISTS ix_tool_provider_configs_org_group_active;

DROP INDEX IF EXISTS ix_tool_provider_configs_project_group_active;
CREATE UNIQUE INDEX ix_tool_provider_configs_project_group_active
    ON tool_provider_configs (project_id, group_name)
    WHERE project_id IS NOT NULL AND is_active = TRUE;

CREATE UNIQUE INDEX IF NOT EXISTS tool_provider_configs_project_provider_idx
    ON tool_provider_configs (project_id, provider_name)
    WHERE project_id IS NOT NULL;

-- ============================================================
-- Phase 5: tool_description_overrides restructure PK
-- ============================================================

ALTER TABLE tool_description_overrides
    ADD COLUMN IF NOT EXISTS id UUID DEFAULT gen_random_uuid();

UPDATE tool_description_overrides SET id = gen_random_uuid() WHERE id IS NULL;

ALTER TABLE tool_description_overrides ALTER COLUMN id SET NOT NULL;

ALTER TABLE tool_description_overrides DROP CONSTRAINT IF EXISTS tool_description_overrides_pkey;

ALTER TABLE tool_description_overrides
    ADD CONSTRAINT tool_description_overrides_pkey PRIMARY KEY (id);

DROP INDEX IF EXISTS ix_tool_description_overrides_project_tool;

CREATE UNIQUE INDEX uq_tool_description_overrides_platform_tool
    ON tool_description_overrides (tool_name)
    WHERE scope = 'platform';

CREATE UNIQUE INDEX uq_tool_description_overrides_project_tool
    ON tool_description_overrides (project_id, tool_name)
    WHERE project_id IS NOT NULL;


-- === 00118_org_to_account.sql ===

-- ============================================================
-- Phase 1: Drop org_invitations (single-user model)
-- ============================================================

DROP TABLE IF EXISTS org_invitations;

-- ============================================================
-- Phase 2: Core table renames  org -> account
-- ============================================================

ALTER TABLE orgs RENAME TO accounts;
ALTER TABLE org_memberships RENAME TO account_memberships;
ALTER TABLE org_entitlement_overrides RENAME TO account_entitlement_overrides;
ALTER TABLE org_settings RENAME TO account_settings;
ALTER TABLE org_feature_overrides RENAME TO account_feature_overrides;

-- ============================================================
-- Phase 3: org_id -> account_id column renames
-- ============================================================

-- renamed tables
ALTER TABLE account_memberships RENAME COLUMN org_id TO account_id;
ALTER TABLE account_entitlement_overrides RENAME COLUMN org_id TO account_id;
ALTER TABLE account_settings RENAME COLUMN org_id TO account_id;
ALTER TABLE account_feature_overrides RENAME COLUMN org_id TO account_id;

-- core domain
ALTER TABLE projects RENAME COLUMN org_id TO account_id;
ALTER TABLE threads RENAME COLUMN org_id TO account_id;
ALTER TABLE runs RENAME COLUMN org_id TO account_id;
ALTER TABLE messages RENAME COLUMN org_id TO account_id;

-- auth / keys
ALTER TABLE api_keys RENAME COLUMN org_id TO account_id;
ALTER TABLE rbac_roles RENAME COLUMN org_id TO account_id;
ALTER TABLE ip_rules RENAME COLUMN org_id TO account_id;

-- credentials
ALTER TABLE llm_credentials RENAME COLUMN org_id TO account_id;
ALTER TABLE llm_routes RENAME COLUMN org_id TO account_id;
ALTER TABLE asr_credentials RENAME COLUMN org_id TO account_id;
ALTER TABLE secrets RENAME COLUMN org_id TO account_id;

-- tools
ALTER TABLE tool_provider_configs RENAME COLUMN org_id TO account_id;
ALTER TABLE tool_description_overrides RENAME COLUMN org_id TO account_id;
ALTER TABLE mcp_configs RENAME COLUMN org_id TO account_id;

-- personas / skills
ALTER TABLE personas RENAME COLUMN org_id TO account_id;
ALTER TABLE skill_packages RENAME COLUMN org_id TO account_id;
ALTER TABLE profile_skill_installs RENAME COLUMN org_id TO account_id;
ALTER TABLE workspace_skill_enablements RENAME COLUMN org_id TO account_id;

-- billing
ALTER TABLE credits RENAME COLUMN org_id TO account_id;
ALTER TABLE credit_transactions RENAME COLUMN org_id TO account_id;
ALTER TABLE subscriptions RENAME COLUMN org_id TO account_id;
ALTER TABLE usage_records RENAME COLUMN org_id TO account_id;
ALTER TABLE redemption_records RENAME COLUMN org_id TO account_id;

-- infra / runtime
ALTER TABLE shell_sessions RENAME COLUMN org_id TO account_id;
ALTER TABLE default_workspace_bindings RENAME COLUMN org_id TO account_id;
ALTER TABLE profile_registries RENAME COLUMN org_id TO account_id;
ALTER TABLE workspace_registries RENAME COLUMN org_id TO account_id;
ALTER TABLE browser_state_registries RENAME COLUMN org_id TO account_id;

-- org-scoped configs
ALTER TABLE teams RENAME COLUMN org_id TO account_id;

-- webhooks / audit / notifications
ALTER TABLE webhook_endpoints RENAME COLUMN org_id TO account_id;
ALTER TABLE webhook_deliveries RENAME COLUMN org_id TO account_id;
ALTER TABLE audit_logs RENAME COLUMN org_id TO account_id;
ALTER TABLE notifications RENAME COLUMN org_id TO account_id;
ALTER TABLE user_memory_snapshots RENAME COLUMN org_id TO account_id;

-- ============================================================
-- Phase 4: Scope cleanup - llm_credentials
-- ============================================================

DROP INDEX IF EXISTS llm_credentials_project_name_idx;
DROP INDEX IF EXISTS llm_credentials_platform_name_idx;

ALTER TABLE llm_credentials DROP COLUMN IF EXISTS scope;

CREATE UNIQUE INDEX llm_credentials_platform_name_idx
    ON llm_credentials (name)
    WHERE owner_kind = 'platform';

-- ============================================================
-- Phase 5: Scope cleanup - secrets
-- ============================================================

DROP INDEX IF EXISTS secrets_org_name_idx;
DROP INDEX IF EXISTS secrets_platform_name_idx;

ALTER TABLE secrets DROP COLUMN IF EXISTS scope;

CREATE UNIQUE INDEX secrets_platform_name_idx
    ON secrets (name)
    WHERE owner_kind = 'platform';

-- ============================================================
-- Phase 6: Scope cleanup - asr_credentials
-- ============================================================

DROP INDEX IF EXISTS asr_credentials_org_name_idx;
DROP INDEX IF EXISTS asr_credentials_org_default_idx;
DROP INDEX IF EXISTS asr_credentials_platform_name_idx;
DROP INDEX IF EXISTS asr_credentials_platform_default_idx;

ALTER TABLE asr_credentials DROP COLUMN IF EXISTS scope;

CREATE UNIQUE INDEX asr_credentials_platform_name_idx
    ON asr_credentials (name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX asr_credentials_platform_default_idx
    ON asr_credentials (is_default)
    WHERE owner_kind = 'platform' AND is_default = true AND revoked_at IS NULL;

-- ============================================================
-- Phase 7: Scope cleanup - tool_provider_configs
-- ============================================================

ALTER TABLE tool_provider_configs
    ADD COLUMN IF NOT EXISTS owner_kind TEXT DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id);

UPDATE tool_provider_configs SET owner_kind = 'platform' WHERE scope = 'platform';

UPDATE tool_provider_configs tpc
SET owner_kind = 'user',
    owner_user_id = p.owner_user_id
FROM projects p
WHERE tpc.scope = 'project' AND tpc.project_id = p.id;

-- remove orphaned project-scoped rows without a matched project
DELETE FROM tool_provider_configs
WHERE scope = 'project' AND owner_kind != 'user';

DROP INDEX IF EXISTS ix_tool_provider_configs_platform_group_active;
DROP INDEX IF EXISTS tool_provider_configs_platform_provider_idx;
DROP INDEX IF EXISTS ix_tool_provider_configs_project_group_active;
DROP INDEX IF EXISTS tool_provider_configs_project_provider_idx;

ALTER TABLE tool_provider_configs DROP COLUMN IF EXISTS scope;
ALTER TABLE tool_provider_configs DROP COLUMN IF EXISTS project_id;

ALTER TABLE tool_provider_configs DROP CONSTRAINT IF EXISTS chk_tool_provider_configs_owner_kind;
ALTER TABLE tool_provider_configs
    ADD CONSTRAINT chk_tool_provider_configs_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX tool_provider_configs_platform_provider_idx
    ON tool_provider_configs (provider_name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX ix_tool_provider_configs_platform_group_active
    ON tool_provider_configs (group_name)
    WHERE owner_kind = 'platform' AND is_active = true;

CREATE UNIQUE INDEX tool_provider_configs_user_provider_idx
    ON tool_provider_configs (owner_user_id, provider_name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

CREATE UNIQUE INDEX ix_tool_provider_configs_user_group_active
    ON tool_provider_configs (owner_user_id, group_name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL AND is_active = TRUE;

-- ============================================================
-- Phase 8: Scope cleanup - tool_description_overrides
-- ============================================================

DROP INDEX IF EXISTS uq_tool_description_overrides_platform_tool;
DROP INDEX IF EXISTS uq_tool_description_overrides_project_tool;

ALTER TABLE tool_description_overrides DROP COLUMN IF EXISTS scope;
ALTER TABLE tool_description_overrides DROP COLUMN IF EXISTS project_id;
ALTER TABLE tool_description_overrides DROP COLUMN IF EXISTS account_id;

CREATE UNIQUE INDEX uq_tool_description_overrides_tool
    ON tool_description_overrides (tool_name);

-- ============================================================
-- Phase 9: Index renames
-- ============================================================

-- account_memberships (was org_memberships)
ALTER INDEX ix_org_memberships_org_id RENAME TO ix_account_memberships_account_id;
ALTER INDEX ix_org_memberships_user_id RENAME TO ix_account_memberships_user_id;

-- account_entitlement_overrides
ALTER INDEX idx_org_entitlement_overrides_org_id RENAME TO idx_account_entitlement_overrides_account_id;

-- account_settings / account_feature_overrides
ALTER INDEX ix_org_settings_key RENAME TO ix_account_settings_key;
ALTER INDEX idx_org_feature_overrides_org_id RENAME TO idx_account_feature_overrides_account_id;

-- projects / threads / runs / messages
ALTER INDEX idx_projects_org_id RENAME TO idx_projects_account_id;
ALTER INDEX ix_threads_org_id RENAME TO ix_threads_account_id;
ALTER INDEX ix_runs_org_id RENAME TO ix_runs_account_id;
ALTER INDEX ix_runs_org_id_created_at_id RENAME TO ix_runs_account_id_created_at_id;
ALTER INDEX ix_messages_org_id_thread_id_created_at RENAME TO ix_messages_account_id_thread_id_created_at;

-- api_keys / credentials / secrets
ALTER INDEX idx_api_keys_org_id RENAME TO idx_api_keys_account_id;
ALTER INDEX ix_llm_credentials_org_id RENAME TO ix_llm_credentials_account_id;
ALTER INDEX ix_llm_routes_org_id RENAME TO ix_llm_routes_account_id;
ALTER INDEX ix_secrets_org_id RENAME TO ix_secrets_account_id;

-- billing
ALTER INDEX idx_credit_transactions_org_created RENAME TO idx_credit_transactions_account_created;
ALTER INDEX uq_subscriptions_org_active RENAME TO uq_subscriptions_account_active;
ALTER INDEX idx_usage_records_org_recorded RENAME TO idx_usage_records_account_recorded;

-- teams / webhooks / audit / ip_rules
ALTER INDEX idx_teams_org_id RENAME TO idx_teams_account_id;
ALTER INDEX idx_webhook_endpoints_org_id RENAME TO idx_webhook_endpoints_account_id;
ALTER INDEX idx_webhook_deliveries_org_id RENAME TO idx_webhook_deliveries_account_id;
ALTER INDEX ix_audit_logs_org_id_ts RENAME TO ix_audit_logs_account_id_ts;
ALTER INDEX idx_ip_rules_org_id RENAME TO idx_ip_rules_account_id;

-- registries
ALTER INDEX idx_profile_registries_org_id RENAME TO idx_profile_registries_account_id;
ALTER INDEX idx_workspace_registries_org_id RENAME TO idx_workspace_registries_account_id;
ALTER INDEX idx_browser_state_registries_org_id RENAME TO idx_browser_state_registries_account_id;

-- shell_sessions
ALTER INDEX idx_shell_sessions_org_thread RENAME TO idx_shell_sessions_account_thread;
ALTER INDEX idx_shell_sessions_org_workspace RENAME TO idx_shell_sessions_account_workspace;
ALTER INDEX idx_shell_sessions_org_run RENAME TO idx_shell_sessions_account_run;
ALTER INDEX idx_shell_sessions_org_run_type RENAME TO idx_shell_sessions_account_run_type;
ALTER INDEX idx_shell_sessions_org_lease_until RENAME TO idx_shell_sessions_account_lease_until;
ALTER INDEX idx_shell_sessions_org_profile_default_binding_updated RENAME TO idx_shell_sessions_account_profile_default_binding_updated;
ALTER INDEX idx_shell_sessions_org_profile_binding_type_unique RENAME TO idx_shell_sessions_account_profile_binding_type_unique;

-- ============================================================
-- Phase 10: Named constraint renames
-- ============================================================

ALTER TABLE accounts RENAME CONSTRAINT uq_orgs_slug TO uq_accounts_slug;
ALTER TABLE account_memberships RENAME CONSTRAINT uq_org_memberships_org_id_user_id TO uq_account_memberships_account_id_user_id;
ALTER TABLE threads RENAME CONSTRAINT uq_threads_id_org_id TO uq_threads_id_account_id;
ALTER TABLE messages RENAME CONSTRAINT fk_messages_org_id_orgs TO fk_messages_account_id_accounts;
ALTER TABLE messages RENAME CONSTRAINT fk_messages_thread_org TO fk_messages_thread_account;
ALTER TABLE mcp_configs RENAME CONSTRAINT uq_mcp_configs_org_name TO uq_mcp_configs_account_name;
ALTER TABLE mcp_configs RENAME CONSTRAINT mcp_configs_org_id_fkey TO mcp_configs_account_id_fkey;
ALTER TABLE skill_packages RENAME CONSTRAINT uq_skill_packages_org_key_version TO uq_skill_packages_account_key_version;

-- ============================================================
-- Phase 11: Rename role values in account_memberships
-- ============================================================

UPDATE account_memberships SET role = 'account_admin' WHERE role = 'org_admin';
UPDATE account_memberships SET role = 'account_member' WHERE role = 'org_member';


-- === 00119_sub_agents.sql ===

CREATE TABLE sub_agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    parent_run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    parent_thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    root_run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    root_thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    depth INTEGER NOT NULL,
    role TEXT NULL,
    persona_id TEXT NULL,
    nickname TEXT NULL,
    source_type TEXT NOT NULL,
    context_mode TEXT NOT NULL,
    status TEXT NOT NULL,
    current_run_id UUID NULL REFERENCES runs(id) ON DELETE SET NULL,
    last_completed_run_id UUID NULL REFERENCES runs(id) ON DELETE SET NULL,
    last_output_ref TEXT NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    closed_at TIMESTAMPTZ NULL,
    CONSTRAINT chk_sub_agents_status CHECK (
        status IN (
            'created',
            'queued',
            'running',
            'waiting_input',
            'completed',
            'failed',
            'cancelled',
            'closed',
            'resumable'
        )
    )
);

CREATE INDEX idx_sub_agents_org_id ON sub_agents(org_id);
CREATE INDEX idx_sub_agents_parent_run_id ON sub_agents(parent_run_id);
CREATE INDEX idx_sub_agents_root_run_id ON sub_agents(root_run_id);
CREATE INDEX idx_sub_agents_current_run_id ON sub_agents(current_run_id) WHERE current_run_id IS NOT NULL;
CREATE INDEX idx_sub_agents_status ON sub_agents(status);


-- === 00120_sub_agent_events.sql ===

CREATE TABLE sub_agent_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sub_agent_id UUID NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    run_id UUID NULL REFERENCES runs(id) ON DELETE SET NULL,
    seq BIGINT NOT NULL DEFAULT nextval('run_events_seq_global'),
    ts TIMESTAMPTZ NOT NULL DEFAULT now(),
    type TEXT NOT NULL,
    data_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_class TEXT NULL,
    CONSTRAINT uq_sub_agent_events_sub_agent_id_seq UNIQUE (sub_agent_id, seq)
);

CREATE INDEX idx_sub_agent_events_sub_agent_id_ts ON sub_agent_events(sub_agent_id, ts);
CREATE INDEX idx_sub_agent_events_type ON sub_agent_events(type);
CREATE INDEX idx_sub_agent_events_run_id ON sub_agent_events(run_id) WHERE run_id IS NOT NULL;


-- === 00121_sub_agent_pending_inputs.sql ===

CREATE TABLE sub_agent_pending_inputs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sub_agent_id UUID NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    seq BIGINT NOT NULL DEFAULT nextval('run_events_seq_global'),
    input TEXT NOT NULL,
    priority BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_sub_agent_pending_inputs_sub_agent_id_seq UNIQUE (sub_agent_id, seq)
);

CREATE INDEX idx_sub_agent_pending_inputs_sub_agent_id_seq
    ON sub_agent_pending_inputs(sub_agent_id, priority DESC, seq ASC);


-- === 00122_sub_agent_context_snapshots.sql ===

CREATE TABLE sub_agent_context_snapshots (
    sub_agent_id UUID PRIMARY KEY REFERENCES sub_agents(id) ON DELETE CASCADE,
    snapshot_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sub_agent_context_snapshots_updated_at
    ON sub_agent_context_snapshots(updated_at);


-- === 00123_personas_roles_json.sql ===

ALTER TABLE personas
    ADD COLUMN roles_json JSONB NOT NULL DEFAULT '{}'::jsonb;


-- === 00124_sub_agents_org_to_account.sql ===

ALTER TABLE sub_agents RENAME COLUMN org_id TO account_id;
ALTER INDEX idx_sub_agents_org_id RENAME TO idx_sub_agents_account_id;
ALTER TABLE sub_agents RENAME CONSTRAINT sub_agents_org_id_fkey TO sub_agents_account_id_fkey;


-- === 00125_seed_system_agent_user.sql ===

-- +goose StatementBegin
-- system_agent: 平台级服务用户，供 Platform Agent 内部调用使用，不可登录。
DO $$
DECLARE
    v_user_id    UUID;
    v_account_id UUID;
BEGIN
    -- 幂等: 如果 system_agent 已存在则跳过
    SELECT id INTO v_user_id FROM users WHERE username = 'system_agent' AND deleted_at IS NULL;
    IF v_user_id IS NOT NULL THEN
        RETURN;
    END IF;

    INSERT INTO users (username, status, is_platform_admin, created_at)
    VALUES ('system_agent', 'active', TRUE, now())
    RETURNING id INTO v_user_id;

    INSERT INTO accounts (slug, name, type, created_at)
    VALUES ('system-agent', 'System Agent', 'personal', now())
    RETURNING id INTO v_account_id;

    INSERT INTO account_memberships (account_id, user_id, role, created_at)
    VALUES (v_account_id, v_user_id, 'platform_admin', now());
END $$;
-- +goose StatementEnd

INSERT INTO rbac_roles (name, permissions, is_system)
VALUES (
    'system_agent',
    ARRAY[
        'data.personas.read', 'data.personas.manage',
        'data.skills.read', 'data.skills.manage',
        'data.llm_credentials.manage',
        'data.mcp_configs.manage',
        'data.projects.read', 'data.projects.manage',
        'data.webhooks.manage',
        'platform.feature_flags.manage'
    ],
    TRUE
)
ON CONFLICT DO NOTHING;


-- === 00126_platform_skills.sql ===

-- Make account_id nullable to support platform-level skills (NULL = platform-owned)
ALTER TABLE skill_packages ALTER COLUMN account_id DROP NOT NULL;

-- Sync mode for platform skill lifecycle management
ALTER TABLE skill_packages ADD COLUMN IF NOT EXISTS sync_mode TEXT NOT NULL DEFAULT 'none';
ALTER TABLE skill_packages ADD CONSTRAINT chk_skill_packages_sync_mode CHECK (sync_mode IN ('none', 'platform_skill'));

-- Content hash for change detection during sync
ALTER TABLE skill_packages ADD COLUMN IF NOT EXISTS content_hash TEXT;

-- Drop dependent foreign keys before replacing unique constraint
ALTER TABLE profile_skill_installs DROP CONSTRAINT IF EXISTS fk_profile_skill_installs_package;
ALTER TABLE workspace_skill_enablements DROP CONSTRAINT IF EXISTS fk_workspace_skill_enablements_package;

-- Replace old unique constraint with one that treats NULLs as equal
ALTER TABLE skill_packages DROP CONSTRAINT IF EXISTS uq_skill_packages_account_key_version;
CREATE UNIQUE INDEX uq_skill_packages_account_key_version ON skill_packages (account_id, skill_key, version) NULLS NOT DISTINCT;

-- Recreate foreign keys referencing the new unique index
ALTER TABLE profile_skill_installs ADD CONSTRAINT fk_profile_skill_installs_package
    FOREIGN KEY (account_id, skill_key, version) REFERENCES skill_packages(account_id, skill_key, version) ON DELETE CASCADE;
ALTER TABLE workspace_skill_enablements ADD CONSTRAINT fk_workspace_skill_enablements_package
    FOREIGN KEY (account_id, skill_key, version) REFERENCES skill_packages(account_id, skill_key, version) ON DELETE CASCADE;


-- === 00127_platform_skill_overrides.sql ===
CREATE TABLE profile_platform_skill_overrides (
    profile_ref  TEXT        NOT NULL,
    skill_key    TEXT        NOT NULL,
    version      TEXT        NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'manual',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (profile_ref, skill_key, version),
    CONSTRAINT chk_platform_skill_override_status CHECK (status IN ('manual', 'removed'))
);

CREATE INDEX idx_platform_skill_overrides_profile
    ON profile_platform_skill_overrides (profile_ref);


-- === 00128_llm_routes_show_in_picker.sql ===

ALTER TABLE llm_routes
    ADD COLUMN IF NOT EXISTS show_in_picker BOOLEAN NOT NULL DEFAULT TRUE;


-- === 00129_personas_core_tools.sql ===

ALTER TABLE personas ADD COLUMN IF NOT EXISTS core_tools TEXT[] NOT NULL DEFAULT '{}';

-- === 00130_channels.sql ===

-- users: 区分注册来源
ALTER TABLE users ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'web';

-- Channel 配置（一行 = 一个 Bot 实例）
CREATE TABLE channels (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel_type    TEXT        NOT NULL,
    persona_id      UUID        REFERENCES personas(id) ON DELETE SET NULL,
    credentials_id  UUID        REFERENCES secrets(id),
    owner_user_id   UUID        REFERENCES users(id) ON DELETE SET NULL,
    webhook_secret  TEXT,
    webhook_url     TEXT,
    is_active       BOOLEAN     NOT NULL DEFAULT FALSE,
    config_json     JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channels_account_type UNIQUE (account_id, channel_type)
);

CREATE INDEX ix_channels_account_id ON channels(account_id);

-- 跨平台统一身份主体
CREATE TABLE channel_identities (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type        TEXT        NOT NULL,
    platform_subject_id TEXT        NOT NULL,
    user_id             UUID        REFERENCES users(id) ON DELETE SET NULL,
    display_name        TEXT,
    avatar_url          TEXT,
    metadata            JSONB       NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_identities_type_subject UNIQUE (channel_type, platform_subject_id)
);

CREATE INDEX ix_channel_identities_user_id ON channel_identities(user_id);

-- 一次性绑定验证码
CREATE TABLE channel_identity_bind_codes (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    token                       TEXT        NOT NULL UNIQUE,
    issued_by_user_id           UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type                TEXT,
    used_at                     TIMESTAMPTZ,
    used_by_channel_identity_id UUID        REFERENCES channel_identities(id),
    expires_at                  TIMESTAMPTZ NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_channel_identity_bind_codes_user ON channel_identity_bind_codes(issued_by_user_id);


-- === 00131_channel_dm_threads_and_receipts.sql ===

CREATE TABLE channel_dm_threads (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id UUID        NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    persona_id          UUID        NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id           UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_dm_threads_binding UNIQUE (channel_id, channel_identity_id, persona_id),
    CONSTRAINT uq_channel_dm_threads_thread UNIQUE (thread_id)
);

CREATE INDEX ix_channel_dm_threads_channel_identity ON channel_dm_threads(channel_identity_id);
CREATE INDEX ix_channel_dm_threads_channel_id ON channel_dm_threads(channel_id);

CREATE TABLE channel_message_receipts (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT        NOT NULL,
    platform_message_id TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_message_receipts UNIQUE (channel_id, platform_chat_id, platform_message_id)
);

CREATE INDEX ix_channel_message_receipts_channel_id ON channel_message_receipts(channel_id);


-- === 00132_channel_group_threads_and_deliveries.sql ===

CREATE TABLE channel_group_threads (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id       UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id TEXT        NOT NULL,
    persona_id       UUID        NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id        UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_group_threads_binding UNIQUE (channel_id, platform_chat_id, persona_id),
    CONSTRAINT uq_channel_group_threads_thread UNIQUE (thread_id)
);

CREATE INDEX ix_channel_group_threads_channel_id ON channel_group_threads(channel_id);

CREATE TABLE channel_message_deliveries (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id              UUID        REFERENCES runs(id) ON DELETE SET NULL,
    thread_id           UUID        REFERENCES threads(id) ON DELETE SET NULL,
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT        NOT NULL,
    platform_message_id TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_message_deliveries UNIQUE (channel_id, platform_chat_id, platform_message_id)
);

CREATE INDEX ix_channel_message_deliveries_run_id ON channel_message_deliveries(run_id);
CREATE INDEX ix_channel_message_deliveries_thread_id ON channel_message_deliveries(thread_id);
CREATE INDEX ix_channel_message_deliveries_channel_id ON channel_message_deliveries(channel_id);


-- === 00133_channel_message_ledger.sql ===

CREATE TABLE channel_message_ledger (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id                  UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_type                TEXT        NOT NULL,
    direction                   TEXT        NOT NULL,
    thread_id                   UUID        REFERENCES threads(id) ON DELETE SET NULL,
    run_id                      UUID        REFERENCES runs(id) ON DELETE SET NULL,
    platform_conversation_id    TEXT        NOT NULL,
    platform_message_id         TEXT        NOT NULL,
    platform_parent_message_id  TEXT,
    platform_thread_id          TEXT,
    sender_channel_identity_id  UUID        REFERENCES channel_identities(id) ON DELETE SET NULL,
    metadata_json               JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ck_channel_message_ledger_direction
        CHECK (direction IN ('inbound', 'outbound')),
    CONSTRAINT uq_channel_message_ledger_entry
        UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
);

CREATE INDEX ix_channel_message_ledger_channel_id ON channel_message_ledger(channel_id);
CREATE INDEX ix_channel_message_ledger_thread_id ON channel_message_ledger(thread_id);
CREATE INDEX ix_channel_message_ledger_run_id ON channel_message_ledger(run_id);
CREATE INDEX ix_channel_message_ledger_sender_identity_id ON channel_message_ledger(sender_channel_identity_id);


-- === 00134_messages_add_compacted.sql ===
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS compacted BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS ix_messages_thread_compacted
    ON messages (thread_id, compacted)
    WHERE deleted_at IS NULL AND compacted = TRUE;


-- === 00135_persona_runtime_sync.sql ===
ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS soul_md TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS user_selectable BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS selector_name TEXT,
    ADD COLUMN IF NOT EXISTS selector_order INTEGER,
    ADD COLUMN IF NOT EXISTS title_summarize_json JSONB,
    ADD COLUMN IF NOT EXISTS sync_mode TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS mirrored_file_dir TEXT,
    ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ;

UPDATE personas
SET updated_at = created_at
WHERE updated_at IS NULL;

ALTER TABLE personas
    DROP CONSTRAINT IF EXISTS chk_personas_sync_mode;

ALTER TABLE personas
    ADD CONSTRAINT chk_personas_sync_mode
        CHECK (sync_mode IN ('none', 'platform_file_mirror'));

DELETE FROM personas
WHERE executor_type = 'agent.lua'
  AND COALESCE(executor_config_json ? 'script', FALSE) = FALSE
  AND COALESCE(executor_config_json ? 'script_file', FALSE) = TRUE;


-- === 00136_seed_claw_feature_flag.sql ===
INSERT INTO feature_flags (key, description, default_value)
VALUES ('claw_enabled', 'enable cloud claw mode', false)
ON CONFLICT (key) DO NOTHING;


-- === 00137_personas_stream_thinking.sql ===
ALTER TABLE personas
	ADD COLUMN IF NOT EXISTS stream_thinking BOOLEAN NOT NULL DEFAULT TRUE;


-- === 00138_heartbeat.sql ===
CREATE TABLE IF NOT EXISTS scheduled_triggers (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_identity_id   UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    persona_key           TEXT NOT NULL,
    account_id            UUID NOT NULL,
    model                 TEXT NOT NULL DEFAULT '',
    interval_min          INT NOT NULL DEFAULT 30,
    next_fire_at          TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_channel_identity_id_idx
    ON scheduled_triggers (channel_identity_id);

CREATE INDEX IF NOT EXISTS scheduled_triggers_next_fire_at_idx
    ON scheduled_triggers (next_fire_at);


-- === 00139_channel_identities_heartbeat.sql ===
ALTER TABLE channel_identities
    ADD COLUMN IF NOT EXISTS heartbeat_enabled INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30,
    ADD COLUMN IF NOT EXISTS heartbeat_model TEXT NOT NULL DEFAULT '';


-- === 00140_channels_owner_user_id.sql ===

ALTER TABLE channels
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;


-- === 00141_channel_identity_links.sql ===

CREATE TABLE IF NOT EXISTS channel_identity_links (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id UUID        NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_identity_links UNIQUE (channel_id, channel_identity_id)
);

CREATE INDEX IF NOT EXISTS ix_channel_identity_links_channel_id
    ON channel_identity_links(channel_id);

CREATE INDEX IF NOT EXISTS ix_channel_identity_links_identity_id
    ON channel_identity_links(channel_identity_id);


-- === 00141_runs_resume_from_and_interrupted.sql ===
ALTER TABLE runs
    ADD COLUMN resume_from_run_id UUID REFERENCES runs(id) ON DELETE SET NULL;

ALTER TABLE runs
    DROP CONSTRAINT IF EXISTS ck_runs_status;

ALTER TABLE runs
    ADD CONSTRAINT ck_runs_status
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling', 'interrupted'));


-- === 00142_channel_binding_heartbeat_scope.sql ===

ALTER TABLE channel_identity_links
    ADD COLUMN IF NOT EXISTS heartbeat_enabled INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30,
    ADD COLUMN IF NOT EXISTS heartbeat_model TEXT NOT NULL DEFAULT '';

UPDATE channel_identity_links AS cil
   SET heartbeat_enabled = COALESCE(ci.heartbeat_enabled, 0),
       heartbeat_interval_minutes = COALESCE(ci.heartbeat_interval_minutes, 30),
       heartbeat_model = COALESCE(ci.heartbeat_model, '')
  FROM channel_identities AS ci
 WHERE ci.id = cil.channel_identity_id;

ALTER TABLE scheduled_triggers
    ADD COLUMN IF NOT EXISTS channel_id UUID;

DROP INDEX IF EXISTS scheduled_triggers_channel_identity_id_idx;

INSERT INTO scheduled_triggers (
    id,
    channel_id,
    channel_identity_id,
    persona_key,
    account_id,
    model,
    interval_min,
    next_fire_at,
    created_at,
    updated_at
)
WITH target_persona AS (
    SELECT DISTINCT ON (account_id, key)
           id,
           account_id,
           key
      FROM personas
     WHERE deleted_at IS NULL
     ORDER BY account_id, key, created_at DESC
)
SELECT
    gen_random_uuid(),
    cgt.channel_id,
    st.channel_identity_id,
    st.persona_key,
    st.account_id,
    st.model,
    st.interval_min,
    st.next_fire_at,
    st.created_at,
    st.updated_at
  FROM scheduled_triggers AS st
  JOIN channel_identities AS ci
    ON ci.id = st.channel_identity_id
  JOIN target_persona AS tp
    ON tp.account_id = st.account_id
   AND tp.key = st.persona_key
  JOIN channel_group_threads AS cgt
    ON cgt.platform_chat_id = ci.platform_subject_id
   AND cgt.persona_id = tp.id
  JOIN threads AS t
    ON t.id = cgt.thread_id
 WHERE st.channel_id IS NULL
   AND t.account_id = st.account_id
   AND t.deleted_at IS NULL;

INSERT INTO scheduled_triggers (
    id,
    channel_id,
    channel_identity_id,
    persona_key,
    account_id,
    model,
    interval_min,
    next_fire_at,
    created_at,
    updated_at
)
SELECT DISTINCT
    gen_random_uuid(),
    cil.channel_id,
    st.channel_identity_id,
    st.persona_key,
    st.account_id,
    st.model,
    st.interval_min,
    st.next_fire_at,
    st.created_at,
    st.updated_at
  FROM scheduled_triggers AS st
  JOIN channel_identity_links AS cil
    ON cil.channel_identity_id = st.channel_identity_id
  JOIN channels AS ch
    ON ch.id = cil.channel_id
 WHERE st.channel_id IS NULL
   AND ch.account_id = st.account_id;

DELETE FROM scheduled_triggers
 WHERE channel_id IS NULL;

ALTER TABLE scheduled_triggers
    ALTER COLUMN channel_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_target_idx
    ON scheduled_triggers (channel_id, channel_identity_id);


-- === 00142_personas_conditional_tools.sql ===
ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS conditional_tools_json JSONB;


-- === 00143_mcp_installs.sql ===
CREATE TABLE profile_mcp_installs (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    install_key            TEXT        NOT NULL,
    account_id             UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    profile_ref            TEXT        NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    display_name           TEXT        NOT NULL,
    source_kind            TEXT        NOT NULL,
    source_uri             TEXT,
    sync_mode              TEXT        NOT NULL DEFAULT 'none',
    transport              TEXT        NOT NULL CHECK (transport IN ('stdio', 'http_sse', 'streamable_http')),
    launch_spec_json       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    auth_headers_secret_id UUID        REFERENCES secrets(id) ON DELETE SET NULL,
    host_requirement       TEXT        NOT NULL,
    discovery_status       TEXT        NOT NULL DEFAULT 'needs_check',
    last_error_code        TEXT,
    last_error_message     TEXT,
    last_checked_at        TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_profile_mcp_installs_profile_install UNIQUE (account_id, profile_ref, install_key)
);

CREATE INDEX idx_profile_mcp_installs_account_profile
    ON profile_mcp_installs (account_id, profile_ref);

CREATE TABLE workspace_mcp_enablements (
    workspace_ref       TEXT        NOT NULL REFERENCES workspace_registries(workspace_ref) ON DELETE CASCADE,
    account_id          UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    install_id          UUID        NOT NULL REFERENCES profile_mcp_installs(id) ON DELETE CASCADE,
    install_key         TEXT        NOT NULL,
    enabled_by_user_id  UUID        REFERENCES users(id) ON DELETE SET NULL,
    enabled             BOOLEAN     NOT NULL DEFAULT FALSE,
    enabled_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_ref, install_id)
);

CREATE INDEX idx_workspace_mcp_enablements_workspace
    ON workspace_mcp_enablements (account_id, workspace_ref, enabled);

INSERT INTO profile_mcp_installs (
    install_key,
    account_id,
    profile_ref,
    display_name,
    source_kind,
    sync_mode,
    transport,
    launch_spec_json,
    auth_headers_secret_id,
    host_requirement,
    discovery_status
)
SELECT
    'legacy_' || substr(replace(lower(m.id::text), '-', ''), 1, 24),
    m.account_id,
    pr.profile_ref,
    m.name,
    'manual_console',
    'none',
    m.transport,
    jsonb_strip_nulls(
        jsonb_build_object(
            'url', m.url,
            'command', m.command,
            'args', COALESCE(m.args_json, '[]'::jsonb),
            'cwd', m.cwd,
            'env', COALESCE(m.env_json, '{}'::jsonb),
            'call_timeout_ms', m.call_timeout_ms
        )
    ),
    m.auth_secret_id,
    CASE
        WHEN m.transport = 'stdio' THEN 'cloud_worker'
        ELSE 'remote_http'
    END,
    'needs_check'
FROM mcp_configs m
JOIN profile_registries pr ON pr.account_id = m.account_id
ON CONFLICT (account_id, profile_ref, install_key) DO NOTHING;

INSERT INTO workspace_mcp_enablements (
    workspace_ref,
    account_id,
    install_id,
    install_key,
    enabled_by_user_id,
    enabled,
    enabled_at,
    created_at,
    updated_at
)
SELECT
    pr.default_workspace_ref,
    pi.account_id,
    pi.id,
    pi.install_key,
    COALESCE(
        pr.owner_user_id,
        (
            SELECT am.user_id
            FROM account_memberships am
            WHERE am.account_id = pr.account_id
            ORDER BY am.created_at ASC
            LIMIT 1
        )
    ),
    TRUE,
    now(),
    now(),
    now()
FROM profile_mcp_installs pi
JOIN profile_registries pr
  ON pr.account_id = pi.account_id
 AND pr.profile_ref = pi.profile_ref
WHERE pr.default_workspace_ref IS NOT NULL
  AND trim(pr.default_workspace_ref) <> ''
  AND COALESCE(
        pr.owner_user_id,
        (
            SELECT am.user_id
            FROM account_memberships am
            WHERE am.account_id = pr.account_id
            ORDER BY am.created_at ASC
            LIMIT 1
        )
    ) IS NOT NULL
ON CONFLICT (workspace_ref, install_id) DO NOTHING;


-- === 00144_thread_compaction_snapshots.sql ===
CREATE TABLE thread_compaction_snapshots (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id             UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id              UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    summary_text           TEXT NOT NULL,
    metadata_json          JSONB NOT NULL DEFAULT '{}'::jsonb,
    supersedes_snapshot_id UUID REFERENCES thread_compaction_snapshots(id) ON DELETE SET NULL,
    is_active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_thread_compaction_snapshots_active_thread
    ON thread_compaction_snapshots(thread_id)
    WHERE is_active = TRUE;

CREATE INDEX ix_thread_compaction_snapshots_thread_created_at
    ON thread_compaction_snapshots(thread_id, created_at DESC);


-- === 00145_personas_result_summarize.sql ===
ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS result_summarize_json JSONB;


-- === 00146_read_provider_unification.sql ===
UPDATE tool_provider_configs
SET group_name = 'read'
WHERE group_name = 'image_understanding';

UPDATE tool_provider_configs
SET provider_name = 'read.minimax'
WHERE provider_name = 'image_understanding.minimax';

UPDATE personas
SET conditional_tools_json = REPLACE(conditional_tools_json::text, '"understand_image"', '"read"')::jsonb
WHERE conditional_tools_json IS NOT NULL
  AND conditional_tools_json::text LIKE '%"understand_image"%';


-- === 00147_storage_governance_ttl.sql ===

CREATE INDEX IF NOT EXISTS ix_channel_message_ledger_created_at
    ON channel_message_ledger(created_at);


-- === 00148_notebook_snapshots.sql ===
CREATE TABLE user_notebook_snapshots (
    account_id UUID NOT NULL,
    user_id UUID NOT NULL,
    agent_id TEXT NOT NULL,
    notebook_block TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, user_id, agent_id)
);


-- === 00149_notebook_entries.sql ===
CREATE TABLE notebook_entries (
    id         UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    account_id UUID NOT NULL,
    user_id    UUID NOT NULL,
    agent_id   TEXT NOT NULL DEFAULT 'default',
    scope      TEXT NOT NULL DEFAULT 'user',
    category   TEXT NOT NULL DEFAULT 'general',
    entry_key  TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notebook_entries_user
    ON notebook_entries (account_id, user_id, agent_id);

CREATE INDEX idx_notebook_entries_scope
    ON notebook_entries (account_id, user_id, agent_id, scope);


-- === 00150_channel_message_ledger_message_id.sql ===
ALTER TABLE channel_message_ledger ADD COLUMN message_id UUID REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX ix_channel_message_ledger_message_id ON channel_message_ledger (message_id) WHERE message_id IS NOT NULL;


-- === 00151_rename_claw_to_work.sql ===
UPDATE threads SET mode = 'work' WHERE mode = 'claw';
ALTER TABLE threads DROP CONSTRAINT chk_threads_mode;
ALTER TABLE threads ADD CONSTRAINT chk_threads_mode CHECK (mode IN ('chat', 'work'));
UPDATE feature_flags SET key = 'work_enabled', description = 'enable work mode' WHERE key = 'claw_enabled';

