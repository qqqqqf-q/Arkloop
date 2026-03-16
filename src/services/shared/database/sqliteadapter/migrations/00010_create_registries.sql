-- State registries: profile_registries, workspace_registries, browser_state_registries

-- +goose Up

CREATE TABLE profile_registries (
    profile_ref             TEXT PRIMARY KEY,
    org_id                  TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    owner_user_id           TEXT,
    latest_manifest_rev     TEXT,
    flush_state             TEXT NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER NOT NULL DEFAULT 0,
    last_flush_failed_at    TEXT,
    last_flush_succeeded_at TEXT,
    lease_holder_id         TEXT,
    lease_until             TEXT,
    default_workspace_ref   TEXT,
    store_key               TEXT,
    last_used_at            TEXT NOT NULL DEFAULT (datetime('now')),
    metadata_json           TEXT NOT NULL DEFAULT '{}',
    created_at              TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at              TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_holder_id IS NULL AND lease_until IS NULL) OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE INDEX idx_profile_registries_org_id ON profile_registries(org_id);

CREATE TABLE workspace_registries (
    workspace_ref               TEXT PRIMARY KEY,
    org_id                      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    owner_user_id               TEXT,
    project_id                  TEXT,
    latest_manifest_rev         TEXT,
    flush_state                 TEXT NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count           INTEGER NOT NULL DEFAULT 0,
    last_flush_failed_at        TEXT,
    last_flush_succeeded_at     TEXT,
    lease_holder_id             TEXT,
    lease_until                 TEXT,
    default_shell_session_ref   TEXT,
    store_key                   TEXT,
    last_used_at                TEXT NOT NULL DEFAULT (datetime('now')),
    metadata_json               TEXT NOT NULL DEFAULT '{}',
    created_at                  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at                  TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_holder_id IS NULL AND lease_until IS NULL) OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE INDEX idx_workspace_registries_org_id ON workspace_registries(org_id);

CREATE TABLE browser_state_registries (
    workspace_ref           TEXT PRIMARY KEY,
    org_id                  TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    owner_user_id           TEXT,
    latest_manifest_rev     TEXT,
    lease_holder_id         TEXT,
    lease_until             TEXT,
    store_key               TEXT,
    flush_state             TEXT NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER NOT NULL DEFAULT 0,
    last_used_at            TEXT NOT NULL DEFAULT (datetime('now')),
    last_flush_failed_at    TEXT,
    last_flush_succeeded_at TEXT,
    metadata_json           TEXT NOT NULL DEFAULT '{}',
    created_at              TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at              TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_holder_id IS NULL AND lease_until IS NULL) OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE INDEX idx_browser_state_registries_org_id ON browser_state_registries(org_id);

-- +goose Down

DROP INDEX IF EXISTS idx_browser_state_registries_org_id;
DROP TABLE IF EXISTS browser_state_registries;
DROP INDEX IF EXISTS idx_workspace_registries_org_id;
DROP TABLE IF EXISTS workspace_registries;
DROP INDEX IF EXISTS idx_profile_registries_org_id;
DROP TABLE IF EXISTS profile_registries;
