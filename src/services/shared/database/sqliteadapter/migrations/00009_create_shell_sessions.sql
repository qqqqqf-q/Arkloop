-- Shell session management: shell_sessions, default_workspace_bindings

-- +goose Up

CREATE TABLE shell_sessions (
    session_ref         TEXT PRIMARY KEY,
    org_id              TEXT NOT NULL,
    profile_ref         TEXT NOT NULL,
    workspace_ref       TEXT NOT NULL,
    project_id          TEXT,
    thread_id           TEXT,
    run_id              TEXT,
    share_scope         TEXT NOT NULL,
    state               TEXT NOT NULL,
    live_session_id     TEXT,
    latest_restore_rev  TEXT,
    last_used_at        TEXT NOT NULL DEFAULT (datetime('now')),
    metadata_json       TEXT NOT NULL DEFAULT '{}',
    default_binding_key TEXT,
    lease_owner_id      TEXT,
    lease_until         TEXT,
    lease_epoch         INTEGER NOT NULL DEFAULT 0,
    session_type        TEXT NOT NULL DEFAULT 'shell' CHECK (session_type IN ('shell', 'browser')),
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_owner_id IS NULL AND lease_until IS NULL) OR (lease_owner_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE INDEX idx_shell_sessions_org_thread ON shell_sessions(org_id, thread_id);
CREATE INDEX idx_shell_sessions_org_workspace ON shell_sessions(org_id, workspace_ref);
CREATE INDEX idx_shell_sessions_org_run ON shell_sessions(org_id, run_id);
CREATE INDEX idx_shell_sessions_org_run_type ON shell_sessions(org_id, run_id, session_type);

CREATE UNIQUE INDEX idx_shell_sessions_org_profile_binding_type_unique
    ON shell_sessions (org_id, profile_ref, session_type, default_binding_key)
    WHERE default_binding_key IS NOT NULL AND state <> 'closed';

CREATE TABLE default_workspace_bindings (
    profile_ref       TEXT NOT NULL,
    owner_user_id     TEXT,
    org_id            TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    binding_scope     TEXT NOT NULL CHECK (binding_scope IN ('project', 'thread')),
    binding_target_id TEXT NOT NULL,
    workspace_ref     TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (org_id, profile_ref, binding_scope, binding_target_id)
);

CREATE UNIQUE INDEX idx_default_workspace_bindings_workspace_ref
    ON default_workspace_bindings (workspace_ref);

-- +goose Down

DROP INDEX IF EXISTS idx_default_workspace_bindings_workspace_ref;
DROP TABLE IF EXISTS default_workspace_bindings;
DROP INDEX IF EXISTS idx_shell_sessions_org_profile_binding_type_unique;
DROP INDEX IF EXISTS idx_shell_sessions_org_run_type;
DROP INDEX IF EXISTS idx_shell_sessions_org_run;
DROP INDEX IF EXISTS idx_shell_sessions_org_workspace;
DROP INDEX IF EXISTS idx_shell_sessions_org_thread;
DROP TABLE IF EXISTS shell_sessions;
