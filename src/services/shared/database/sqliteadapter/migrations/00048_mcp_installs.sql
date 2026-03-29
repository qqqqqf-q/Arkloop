-- +goose Up
CREATE TABLE IF NOT EXISTS profile_mcp_installs (
    id                     TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    install_key            TEXT NOT NULL,
    account_id             TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    profile_ref            TEXT NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    display_name           TEXT NOT NULL,
    source_kind            TEXT NOT NULL,
    source_uri             TEXT,
    sync_mode              TEXT NOT NULL DEFAULT 'none',
    transport              TEXT NOT NULL CHECK (transport IN ('stdio', 'http_sse', 'streamable_http')),
    launch_spec_json       TEXT NOT NULL DEFAULT '{}',
    auth_headers_secret_id TEXT REFERENCES secrets(id) ON DELETE SET NULL,
    host_requirement       TEXT NOT NULL,
    discovery_status       TEXT NOT NULL DEFAULT 'needs_check',
    last_error_code        TEXT,
    last_error_message     TEXT,
    last_checked_at        TEXT,
    created_at             TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at             TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, profile_ref, install_key)
);

CREATE INDEX IF NOT EXISTS idx_profile_mcp_installs_account_profile
    ON profile_mcp_installs (account_id, profile_ref);

CREATE TABLE IF NOT EXISTS workspace_mcp_enablements (
    workspace_ref       TEXT NOT NULL REFERENCES workspace_registries(workspace_ref) ON DELETE CASCADE,
    account_id          TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    install_id          TEXT NOT NULL REFERENCES profile_mcp_installs(id) ON DELETE CASCADE,
    install_key         TEXT NOT NULL,
    enabled_by_user_id  TEXT REFERENCES users(id) ON DELETE SET NULL,
    enabled             INTEGER NOT NULL DEFAULT 0,
    enabled_at          TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (workspace_ref, install_id)
);

CREATE INDEX IF NOT EXISTS idx_workspace_mcp_enablements_workspace
    ON workspace_mcp_enablements (account_id, workspace_ref, enabled);

-- Legacy desktop DBs may lack UNIQUE(account_id, profile_ref, install_key); avoid ON CONFLICT.
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
    src.install_key,
    src.account_id,
    src.profile_ref,
    src.display_name,
    src.source_kind,
    src.sync_mode,
    src.transport,
    src.launch_spec_json,
    src.auth_headers_secret_id,
    src.host_requirement,
    src.discovery_status
FROM (
    SELECT
        'legacy_' || substr(replace(lower(m.id), '-', ''), 1, 24) AS install_key,
        m.account_id,
        pr.profile_ref,
        m.name AS display_name,
        'manual_console' AS source_kind,
        'none' AS sync_mode,
        m.transport,
        json_object(
            'url', m.url,
            'command', m.command,
            'args', json(COALESCE(m.args_json, '[]')),
            'cwd', m.cwd,
            'env', json(COALESCE(m.env_json, '{}')),
            'call_timeout_ms', m.call_timeout_ms
        ) AS launch_spec_json,
        m.auth_secret_id AS auth_headers_secret_id,
        CASE
            WHEN m.transport = 'stdio' THEN 'cloud_worker'
            ELSE 'remote_http'
        END AS host_requirement,
        'needs_check' AS discovery_status
    FROM mcp_configs m
    JOIN profile_registries pr ON pr.account_id = m.account_id
) AS src
WHERE NOT EXISTS (
    SELECT 1
      FROM profile_mcp_installs e
     WHERE e.account_id = src.account_id
       AND e.profile_ref = src.profile_ref
       AND e.install_key = src.install_key
);

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
    1,
    datetime('now'),
    datetime('now'),
    datetime('now')
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
  AND NOT EXISTS (
    SELECT 1
      FROM workspace_mcp_enablements w
     WHERE w.workspace_ref = pr.default_workspace_ref
       AND w.install_id = pi.id
);

-- +goose Down
DROP INDEX IF EXISTS idx_workspace_mcp_enablements_workspace;
DROP TABLE IF EXISTS workspace_mcp_enablements;
DROP INDEX IF EXISTS idx_profile_mcp_installs_account_profile;
DROP TABLE IF EXISTS profile_mcp_installs;
