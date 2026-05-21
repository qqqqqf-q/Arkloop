-- +goose Up

CREATE TABLE tool_provider_oauth_connections (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_kind       TEXT        NOT NULL CHECK (owner_kind IN ('platform', 'user')),
    owner_user_id    UUID        REFERENCES users(id) ON DELETE CASCADE,
    group_name       TEXT        NOT NULL,
    provider_name    TEXT        NOT NULL,
    token_secret_id  UUID        NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    client_id        TEXT,
    scope            TEXT,
    expires_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tool_provider_oauth_connections_owner_check
        CHECK ((owner_kind = 'platform' AND owner_user_id IS NULL) OR (owner_kind = 'user' AND owner_user_id IS NOT NULL))
);

CREATE UNIQUE INDEX tool_provider_oauth_connections_platform_idx
    ON tool_provider_oauth_connections (group_name, provider_name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX tool_provider_oauth_connections_user_idx
    ON tool_provider_oauth_connections (owner_user_id, group_name, provider_name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

CREATE TABLE tool_provider_oauth_flows (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_kind              TEXT        NOT NULL CHECK (owner_kind IN ('platform', 'user')),
    owner_user_id           UUID        REFERENCES users(id) ON DELETE CASCADE,
    group_name              TEXT        NOT NULL,
    provider_name           TEXT        NOT NULL,
    state                   TEXT        NOT NULL UNIQUE,
    redirect_uri            TEXT        NOT NULL,
    authorization_url       TEXT        NOT NULL,
    code_verifier_secret_id UUID        NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    client_id               TEXT,
    scope                   TEXT,
    expires_at              TIMESTAMPTZ NOT NULL,
    completed_at            TIMESTAMPTZ,
    connection_id           UUID        REFERENCES tool_provider_oauth_connections(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tool_provider_oauth_flows_owner_check
        CHECK ((owner_kind = 'platform' AND owner_user_id IS NULL) OR (owner_kind = 'user' AND owner_user_id IS NOT NULL))
);

CREATE INDEX tool_provider_oauth_flows_owner_idx
    ON tool_provider_oauth_flows (owner_kind, owner_user_id, group_name, provider_name);

CREATE INDEX tool_provider_oauth_flows_expires_idx
    ON tool_provider_oauth_flows (expires_at);

-- +goose Down

DROP INDEX IF EXISTS tool_provider_oauth_flows_expires_idx;
DROP INDEX IF EXISTS tool_provider_oauth_flows_owner_idx;
DROP TABLE IF EXISTS tool_provider_oauth_flows;
DROP INDEX IF EXISTS tool_provider_oauth_connections_user_idx;
DROP INDEX IF EXISTS tool_provider_oauth_connections_platform_idx;
DROP TABLE IF EXISTS tool_provider_oauth_connections;
