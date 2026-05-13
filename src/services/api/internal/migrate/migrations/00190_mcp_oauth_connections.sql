-- +goose Up

CREATE TABLE mcp_oauth_connections (
    id                            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id                    UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    profile_ref                   TEXT        NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    install_id                    UUID        NOT NULL REFERENCES profile_mcp_installs(id) ON DELETE CASCADE,
    token_secret_id               UUID        NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    client_id                     TEXT,
    client_secret_secret_id       UUID        REFERENCES secrets(id) ON DELETE SET NULL,
    registration_client_uri       TEXT,
    registration_access_secret_id UUID        REFERENCES secrets(id) ON DELETE SET NULL,
    scope                         TEXT,
    expires_at                    TIMESTAMPTZ,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_mcp_oauth_connections_install UNIQUE (account_id, profile_ref, install_id)
);

CREATE INDEX idx_mcp_oauth_connections_account_profile
    ON mcp_oauth_connections (account_id, profile_ref);

CREATE TABLE mcp_oauth_flows (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id              UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    profile_ref             TEXT        NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    install_id              UUID        NOT NULL REFERENCES profile_mcp_installs(id) ON DELETE CASCADE,
    state                   TEXT        NOT NULL UNIQUE,
    redirect_uri            TEXT        NOT NULL,
    authorization_url       TEXT        NOT NULL,
    code_verifier_secret_id UUID        NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    client_id               TEXT,
    client_secret_secret_id UUID        REFERENCES secrets(id) ON DELETE SET NULL,
    scope                   TEXT,
    expires_at              TIMESTAMPTZ NOT NULL,
    completed_at            TIMESTAMPTZ,
    connection_id           UUID        REFERENCES mcp_oauth_connections(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_mcp_oauth_flows_account_install
    ON mcp_oauth_flows (account_id, profile_ref, install_id);

CREATE INDEX idx_mcp_oauth_flows_expires
    ON mcp_oauth_flows (expires_at);

-- +goose Down

DROP INDEX IF EXISTS idx_mcp_oauth_flows_expires;
DROP INDEX IF EXISTS idx_mcp_oauth_flows_account_install;
DROP TABLE IF EXISTS mcp_oauth_flows;
DROP INDEX IF EXISTS idx_mcp_oauth_connections_account_profile;
DROP TABLE IF EXISTS mcp_oauth_connections;
