-- +goose Up
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

-- +goose Down
DROP INDEX IF EXISTS idx_workspace_mcp_enablements_workspace;
DROP TABLE IF EXISTS workspace_mcp_enablements;
DROP INDEX IF EXISTS idx_profile_mcp_installs_account_profile;
DROP TABLE IF EXISTS profile_mcp_installs;
