-- Align desktop SQLite LLM/secrets tables with the current owner-scoped repos.
-- Fixes desktop onboarding/provider validation, which now uses owner_kind /
-- owner_user_id for secrets and llm_credentials, and extended pricing columns
-- on llm_routes.

-- +goose NO TRANSACTION

-- +goose Up

PRAGMA foreign_keys = OFF;

-- Ensure a default project exists so legacy account-scoped routes remain
-- visible after we move them to project scope.
INSERT INTO projects (account_id, owner_user_id, name, visibility, is_default, updated_at)
SELECT
  '00000000-0000-4000-8000-000000000002',
  '00000000-0000-4000-8000-000000000001',
  'Default',
  'private',
  1,
  datetime('now')
WHERE NOT EXISTS (
  SELECT 1
  FROM projects
  WHERE account_id = '00000000-0000-4000-8000-000000000002'
    AND owner_user_id = '00000000-0000-4000-8000-000000000001'
    AND is_default = 1
);

ALTER TABLE secrets RENAME TO secrets_legacy_00020;

CREATE TABLE secrets (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
    owner_kind      TEXT NOT NULL DEFAULT 'platform',
    owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    encrypted_value TEXT NOT NULL,
    key_version     INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    rotated_at      TEXT
);

INSERT INTO secrets (
    id,
    account_id,
    owner_kind,
    owner_user_id,
    name,
    encrypted_value,
    key_version,
    created_at,
    updated_at,
    rotated_at
)
SELECT
    id,
    COALESCE(account_id, '00000000-0000-4000-8000-000000000002'),
    'user',
    '00000000-0000-4000-8000-000000000001',
    name,
    encrypted_value,
    COALESCE(key_version, 1),
    COALESCE(created_at, datetime('now')),
    COALESCE(updated_at, COALESCE(created_at, datetime('now'))),
    NULL
FROM secrets_legacy_00020;

CREATE UNIQUE INDEX secrets_platform_name_idx
    ON secrets (name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX secrets_user_name_idx
    ON secrets (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

DROP TABLE secrets_legacy_00020;

ALTER TABLE llm_credentials RENAME TO llm_credentials_legacy_00020;

DROP INDEX IF EXISTS ix_llm_credentials_org_id;

CREATE TABLE llm_credentials (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
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
    owner_kind      TEXT NOT NULL DEFAULT 'platform',
    owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE
);

INSERT INTO llm_credentials (
    id,
    account_id,
    provider,
    name,
    secret_id,
    key_prefix,
    base_url,
    openai_api_mode,
    advanced_json,
    revoked_at,
    last_used_at,
    created_at,
    updated_at,
    owner_kind,
    owner_user_id
)
SELECT
    id,
    COALESCE(account_id, '00000000-0000-4000-8000-000000000002'),
    provider,
    name,
    secret_id,
    key_prefix,
    base_url,
    openai_api_mode,
    COALESCE(advanced_json, '{}'),
    revoked_at,
    last_used_at,
    COALESCE(created_at, datetime('now')),
    COALESCE(updated_at, COALESCE(created_at, datetime('now'))),
    CASE WHEN owner_kind = 'platform' THEN 'platform' ELSE 'user' END,
    CASE
      WHEN owner_kind = 'platform' THEN NULL
      ELSE COALESCE(owner_user_id, '00000000-0000-4000-8000-000000000001')
    END
FROM llm_credentials_legacy_00020;

CREATE INDEX ix_llm_credentials_account_id ON llm_credentials(account_id);

CREATE UNIQUE INDEX llm_credentials_platform_name_idx
    ON llm_credentials (name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX llm_credentials_user_name_idx
    ON llm_credentials (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

DROP TABLE llm_credentials_legacy_00020;

ALTER TABLE llm_routes RENAME TO llm_routes_legacy_00020;

DROP INDEX IF EXISTS ix_llm_routes_org_id;
DROP INDEX IF EXISTS ix_llm_routes_credential_id;

CREATE TABLE llm_routes (
    id                     TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id             TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
    project_id             TEXT REFERENCES projects(id) ON DELETE CASCADE,
    credential_id          TEXT NOT NULL REFERENCES llm_credentials(id) ON DELETE CASCADE,
    model                  TEXT NOT NULL,
    priority               INTEGER NOT NULL DEFAULT 0,
    is_default             INTEGER NOT NULL DEFAULT 0,
    tags                   TEXT NOT NULL DEFAULT '[]',
    when_json              TEXT NOT NULL DEFAULT '{}',
    advanced_json          TEXT NOT NULL DEFAULT '{}',
    multiplier             REAL NOT NULL DEFAULT 1.0,
    cost_per_1k_input      REAL,
    cost_per_1k_output     REAL,
    cost_per_1k_cache_write REAL,
    cost_per_1k_cache_read REAL,
    created_at             TEXT NOT NULL DEFAULT (datetime('now')),
    route_key              TEXT
);

INSERT INTO llm_routes (
    id,
    account_id,
    project_id,
    credential_id,
    model,
    priority,
    is_default,
    tags,
    when_json,
    advanced_json,
    multiplier,
    cost_per_1k_input,
    cost_per_1k_output,
    cost_per_1k_cache_write,
    cost_per_1k_cache_read,
    created_at,
    route_key
)
SELECT
    r.id,
    COALESCE(r.account_id, '00000000-0000-4000-8000-000000000002'),
    CASE
      WHEN c.owner_kind = 'platform' THEN NULL
      ELSE COALESCE(
        r.project_id,
        (
          SELECT id
          FROM projects
          WHERE account_id = '00000000-0000-4000-8000-000000000002'
            AND owner_user_id = '00000000-0000-4000-8000-000000000001'
            AND is_default = 1
          LIMIT 1
        )
      )
    END,
    r.credential_id,
    r.model,
    COALESCE(r.priority, 0),
    COALESCE(r.is_default, 0),
    COALESCE(r.tags, '[]'),
    COALESCE(r.when_json, '{}'),
    COALESCE(r.advanced_json, '{}'),
    1.0,
    NULL,
    NULL,
    NULL,
    NULL,
    COALESCE(r.created_at, datetime('now')),
    r.route_key
FROM llm_routes_legacy_00020 r
LEFT JOIN llm_credentials c
  ON c.id = r.credential_id;

CREATE INDEX ix_llm_routes_account_id ON llm_routes(account_id);
CREATE INDEX ix_llm_routes_credential_id ON llm_routes(credential_id);
CREATE INDEX ix_llm_routes_project_id
    ON llm_routes(project_id)
    WHERE project_id IS NOT NULL;

CREATE UNIQUE INDEX ux_llm_routes_credential_model_lower
    ON llm_routes (credential_id, lower(model));

CREATE UNIQUE INDEX ux_llm_routes_credential_default
    ON llm_routes (credential_id)
    WHERE is_default = 1;

CREATE UNIQUE INDEX ux_llm_routes_route_key
    ON llm_routes (lower(route_key))
    WHERE route_key IS NOT NULL;

DROP TABLE llm_routes_legacy_00020;

PRAGMA foreign_keys = ON;

-- +goose Down

PRAGMA foreign_keys = OFF;

ALTER TABLE llm_routes RENAME TO llm_routes_rollback_00020;

DROP INDEX IF EXISTS ix_llm_routes_account_id;
DROP INDEX IF EXISTS ix_llm_routes_credential_id;
DROP INDEX IF EXISTS ix_llm_routes_project_id;
DROP INDEX IF EXISTS ux_llm_routes_credential_model_lower;
DROP INDEX IF EXISTS ux_llm_routes_credential_default;
DROP INDEX IF EXISTS ux_llm_routes_route_key;

CREATE TABLE llm_routes (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL REFERENCES llm_credentials(id) ON DELETE CASCADE,
    model         TEXT NOT NULL,
    priority      INTEGER NOT NULL DEFAULT 0,
    is_default    INTEGER NOT NULL DEFAULT 0,
    when_json     TEXT NOT NULL DEFAULT '{}',
    tags          TEXT NOT NULL DEFAULT '[]',
    advanced_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    project_id    TEXT,
    route_key     TEXT,
    UNIQUE (credential_id, model)
);

INSERT INTO llm_routes (
    id,
    account_id,
    credential_id,
    model,
    priority,
    is_default,
    when_json,
    tags,
    advanced_json,
    created_at,
    project_id,
    route_key
)
SELECT
    id,
    account_id,
    credential_id,
    model,
    priority,
    is_default,
    when_json,
    tags,
    advanced_json,
    created_at,
    project_id,
    route_key
FROM llm_routes_rollback_00020;

CREATE INDEX ix_llm_routes_org_id ON llm_routes(account_id);
CREATE INDEX ix_llm_routes_credential_id ON llm_routes(credential_id);

DROP TABLE llm_routes_rollback_00020;

ALTER TABLE llm_credentials RENAME TO llm_credentials_rollback_00020;

DROP INDEX IF EXISTS ix_llm_credentials_account_id;
DROP INDEX IF EXISTS llm_credentials_platform_name_idx;
DROP INDEX IF EXISTS llm_credentials_user_name_idx;

CREATE TABLE llm_credentials (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
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
    owner_kind      TEXT NOT NULL DEFAULT 'account',
    owner_user_id   TEXT,
    UNIQUE (account_id, name)
);

INSERT INTO llm_credentials (
    id,
    account_id,
    provider,
    name,
    secret_id,
    key_prefix,
    base_url,
    openai_api_mode,
    advanced_json,
    revoked_at,
    last_used_at,
    created_at,
    updated_at,
    owner_kind,
    owner_user_id
)
SELECT
    id,
    account_id,
    provider,
    name,
    secret_id,
    key_prefix,
    base_url,
    openai_api_mode,
    advanced_json,
    revoked_at,
    last_used_at,
    created_at,
    updated_at,
    CASE WHEN owner_kind = 'platform' THEN 'platform' ELSE 'account' END,
    owner_user_id
FROM llm_credentials_rollback_00020;

CREATE INDEX ix_llm_credentials_org_id ON llm_credentials(account_id);

DROP TABLE llm_credentials_rollback_00020;

ALTER TABLE secrets RENAME TO secrets_rollback_00020;

DROP INDEX IF EXISTS secrets_platform_name_idx;
DROP INDEX IF EXISTS secrets_user_name_idx;

CREATE TABLE secrets (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    encrypted_value TEXT NOT NULL,
    key_version     INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(account_id, name)
);

INSERT INTO secrets (
    id,
    account_id,
    name,
    encrypted_value,
    key_version,
    created_at,
    updated_at
)
SELECT
    id,
    account_id,
    name,
    encrypted_value,
    key_version,
    created_at,
    updated_at
FROM secrets_rollback_00020;

DROP TABLE secrets_rollback_00020;

PRAGMA foreign_keys = ON;
