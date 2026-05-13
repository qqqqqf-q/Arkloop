-- +goose Up

CREATE TABLE plugin_enablements (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id         TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    package_id         TEXT NOT NULL REFERENCES plugin_packages(id) ON DELETE CASCADE,
    plugin_id          TEXT NOT NULL,
    plugin_version     TEXT NOT NULL,
    profile_ref        TEXT NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    workspace_ref      TEXT NOT NULL REFERENCES workspace_registries(workspace_ref) ON DELETE CASCADE,
    desired_enabled    INTEGER NOT NULL DEFAULT 0,
    enabled_by_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    settings_json      TEXT NOT NULL DEFAULT '{}',
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, package_id, profile_ref, workspace_ref)
);

CREATE INDEX idx_plugin_enablements_account_plugin
    ON plugin_enablements (account_id, plugin_id, plugin_version);

CREATE INDEX idx_plugin_enablements_workspace
    ON plugin_enablements (account_id, workspace_ref, desired_enabled);

CREATE TABLE plugin_runtime_state (
    account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    package_id     TEXT NOT NULL REFERENCES plugin_packages(id) ON DELETE CASCADE,
    plugin_id      TEXT NOT NULL,
    plugin_version TEXT NOT NULL,
    profile_ref    TEXT NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    workspace_ref  TEXT NOT NULL REFERENCES workspace_registries(workspace_ref) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'not_installed',
    status_json    TEXT NOT NULL DEFAULT '{}',
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, package_id, profile_ref, workspace_ref)
);

CREATE INDEX idx_plugin_runtime_state_account_plugin
    ON plugin_runtime_state (account_id, plugin_id, plugin_version);

-- +goose Down

DROP INDEX IF EXISTS idx_plugin_runtime_state_account_plugin;
DROP TABLE IF EXISTS plugin_runtime_state;
DROP INDEX IF EXISTS idx_plugin_enablements_workspace;
DROP INDEX IF EXISTS idx_plugin_enablements_account_plugin;
DROP TABLE IF EXISTS plugin_enablements;
