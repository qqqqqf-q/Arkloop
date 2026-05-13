-- +goose Up

CREATE TABLE mcp_oauth_connections (
    id                            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id                    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    profile_ref                   TEXT NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    install_id                    TEXT NOT NULL REFERENCES profile_mcp_installs(id) ON DELETE CASCADE,
    token_secret_id               TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    client_id                     TEXT,
    client_secret_secret_id       TEXT REFERENCES secrets(id) ON DELETE SET NULL,
    registration_client_uri       TEXT,
    registration_access_secret_id TEXT REFERENCES secrets(id) ON DELETE SET NULL,
    scope                         TEXT,
    expires_at                    TEXT,
    created_at                    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at                    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, profile_ref, install_id)
);

CREATE INDEX idx_mcp_oauth_connections_account_profile
    ON mcp_oauth_connections (account_id, profile_ref);

CREATE TABLE mcp_oauth_flows (
    id                      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id              TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    profile_ref             TEXT NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    install_id              TEXT NOT NULL REFERENCES profile_mcp_installs(id) ON DELETE CASCADE,
    state                   TEXT NOT NULL UNIQUE,
    redirect_uri            TEXT NOT NULL,
    authorization_url       TEXT NOT NULL,
    code_verifier_secret_id TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    client_id               TEXT,
    client_secret_secret_id TEXT REFERENCES secrets(id) ON DELETE SET NULL,
    scope                   TEXT,
    expires_at              TEXT NOT NULL,
    completed_at            TEXT,
    connection_id           TEXT REFERENCES mcp_oauth_connections(id) ON DELETE SET NULL,
    created_at              TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at              TEXT NOT NULL DEFAULT (datetime('now'))
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
