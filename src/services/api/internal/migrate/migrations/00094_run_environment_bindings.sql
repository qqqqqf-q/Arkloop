-- +goose Up

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

-- +goose Down

DROP INDEX IF EXISTS idx_default_workspace_bindings_workspace_ref;
DROP INDEX IF EXISTS idx_default_workspace_bindings_owner_user_id;
DROP TABLE IF EXISTS default_workspace_bindings;

ALTER TABLE runs
    DROP COLUMN IF EXISTS workspace_ref,
    DROP COLUMN IF EXISTS profile_ref;
