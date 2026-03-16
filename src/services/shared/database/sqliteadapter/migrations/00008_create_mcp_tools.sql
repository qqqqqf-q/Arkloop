-- MCP and tool configuration tables: mcp_configs, tool_provider_configs, tool_description_overrides

-- +goose Up

CREATE TABLE mcp_configs (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id             TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    transport          TEXT NOT NULL CHECK (transport IN ('stdio', 'http_sse', 'streamable_http')),
    url                TEXT,
    auth_secret_id     TEXT,
    command            TEXT,
    args_json          TEXT NOT NULL DEFAULT '[]',
    cwd                TEXT,
    env_json           TEXT NOT NULL DEFAULT '{}',
    inherit_parent_env INTEGER NOT NULL DEFAULT 0,
    call_timeout_ms    INTEGER NOT NULL DEFAULT 10000 CHECK (call_timeout_ms > 0),
    is_active          INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (org_id, name)
);

CREATE TABLE tool_provider_configs (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    group_name    TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    is_active     INTEGER NOT NULL DEFAULT 0,
    secret_id     TEXT,
    key_prefix    TEXT,
    base_url      TEXT,
    config_json   TEXT NOT NULL DEFAULT '{}',
    scope         TEXT NOT NULL DEFAULT 'org',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (org_id, provider_name)
);

CREATE UNIQUE INDEX ix_tool_provider_configs_org_group_active
    ON tool_provider_configs (org_id, group_name)
    WHERE is_active = 1;

CREATE TABLE tool_description_overrides (
    org_id      TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    scope       TEXT NOT NULL DEFAULT 'platform',
    tool_name   TEXT NOT NULL,
    description TEXT NOT NULL,
    is_disabled INTEGER NOT NULL DEFAULT 0,
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (org_id, scope, tool_name)
);

-- +goose Down

DROP TABLE IF EXISTS tool_description_overrides;
DROP INDEX IF EXISTS ix_tool_provider_configs_org_group_active;
DROP TABLE IF EXISTS tool_provider_configs;
DROP TABLE IF EXISTS mcp_configs;
