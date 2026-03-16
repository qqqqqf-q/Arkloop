-- LLM credential and routing tables: llm_credentials, llm_routes

-- +goose Up

CREATE TABLE llm_credentials (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL CHECK (provider IN ('openai', 'anthropic', 'gemini', 'deepseek')),
    name            TEXT NOT NULL,
    secret_id       TEXT,
    key_prefix      TEXT,
    base_url        TEXT,
    openai_api_mode TEXT,
    advanced_json   TEXT NOT NULL DEFAULT '{}',
    revoked_at      TEXT,
    last_used_at    TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (org_id, name)
);

CREATE INDEX ix_llm_credentials_org_id ON llm_credentials(org_id);

CREATE TABLE llm_routes (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL REFERENCES llm_credentials(id) ON DELETE CASCADE,
    model         TEXT NOT NULL,
    priority      INTEGER NOT NULL DEFAULT 0,
    is_default    INTEGER NOT NULL DEFAULT 0,
    when_json     TEXT NOT NULL DEFAULT '{}',
    tags          TEXT NOT NULL DEFAULT '[]',
    advanced_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (credential_id, model)
);

CREATE INDEX ix_llm_routes_org_id ON llm_routes(org_id);
CREATE INDEX ix_llm_routes_credential_id ON llm_routes(credential_id);

-- +goose Down

DROP INDEX IF EXISTS ix_llm_routes_credential_id;
DROP INDEX IF EXISTS ix_llm_routes_org_id;
DROP TABLE IF EXISTS llm_routes;
DROP INDEX IF EXISTS ix_llm_credentials_org_id;
DROP TABLE IF EXISTS llm_credentials;
