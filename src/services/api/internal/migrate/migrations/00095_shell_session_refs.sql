-- +goose Up

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

-- +goose Down

DROP INDEX IF EXISTS idx_default_shell_session_bindings_session_ref;
DROP TABLE IF EXISTS default_shell_session_bindings;

DROP INDEX IF EXISTS idx_shell_sessions_org_run;
DROP INDEX IF EXISTS idx_shell_sessions_org_workspace;
DROP INDEX IF EXISTS idx_shell_sessions_org_thread;
DROP TABLE IF EXISTS shell_sessions;
