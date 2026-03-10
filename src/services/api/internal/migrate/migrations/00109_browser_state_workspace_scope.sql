-- +goose Up

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

-- +goose Down

DROP INDEX IF EXISTS idx_browser_state_registries_org_id;
DROP TABLE IF EXISTS browser_state_registries;

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
