-- +goose Up

PRAGMA foreign_keys = OFF;

DROP INDEX IF EXISTS tool_provider_configs_platform_provider_idx;
DROP INDEX IF EXISTS ix_tool_provider_configs_platform_group_active;
DROP INDEX IF EXISTS tool_provider_configs_user_provider_idx;
DROP INDEX IF EXISTS ix_tool_provider_configs_user_group_active;

ALTER TABLE tool_provider_configs RENAME TO tool_provider_configs_legacy_00098;

CREATE TABLE tool_provider_configs (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT REFERENCES accounts(id) ON DELETE CASCADE,
    owner_kind      TEXT NOT NULL DEFAULT 'platform' CHECK (owner_kind IN ('platform', 'user')),
    owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE,
    group_name      TEXT NOT NULL,
    provider_name   TEXT NOT NULL,
    is_active       INTEGER NOT NULL DEFAULT 0,
    secret_id       TEXT,
    key_prefix      TEXT,
    base_url        TEXT,
    config_json     TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO tool_provider_configs (
    id, account_id, owner_kind, owner_user_id, group_name, provider_name,
    is_active, secret_id, key_prefix, base_url, config_json, created_at, updated_at
)
SELECT
    id, account_id, owner_kind, owner_user_id, group_name, provider_name,
    is_active, secret_id, key_prefix, base_url, config_json, created_at, updated_at
FROM tool_provider_configs_legacy_00098;

DROP TABLE tool_provider_configs_legacy_00098;

CREATE UNIQUE INDEX tool_provider_configs_platform_provider_idx
    ON tool_provider_configs (provider_name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX ix_tool_provider_configs_platform_group_active
    ON tool_provider_configs (group_name)
    WHERE owner_kind = 'platform' AND is_active = 1;

CREATE UNIQUE INDEX tool_provider_configs_user_provider_idx
    ON tool_provider_configs (owner_user_id, provider_name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

CREATE UNIQUE INDEX ix_tool_provider_configs_user_group_active
    ON tool_provider_configs (owner_user_id, group_name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL AND is_active = 1;

PRAGMA foreign_keys = ON;

UPDATE tool_provider_configs
SET is_active = 1,
    config_json = json_set(COALESCE(config_json, '{}'), '$.arkloop_default_seed', 'activated'),
    updated_at = datetime('now')
WHERE owner_kind = 'platform'
  AND provider_name = 'web_search.exa'
  AND NOT EXISTS (
      SELECT 1
      FROM tool_provider_configs active
      WHERE active.owner_kind = 'platform'
        AND active.group_name = 'web_search'
        AND active.is_active = 1
  );

INSERT INTO tool_provider_configs (
    account_id,
    owner_kind,
    owner_user_id,
    group_name,
    provider_name,
    is_active,
    secret_id,
    key_prefix,
    base_url,
    config_json
)
SELECT
    NULL,
    'platform',
    NULL,
    'web_search',
    'web_search.exa',
    1,
    NULL,
    NULL,
    NULL,
    '{"arkloop_default_seed":"inserted"}'
WHERE NOT EXISTS (
      SELECT 1
      FROM tool_provider_configs active
      WHERE active.owner_kind = 'platform'
        AND active.group_name = 'web_search'
        AND active.is_active = 1
  )
  AND NOT EXISTS (
      SELECT 1
      FROM tool_provider_configs exa
      WHERE exa.owner_kind = 'platform'
        AND exa.provider_name = 'web_search.exa'
  );

-- +goose Down

DELETE FROM tool_provider_configs
WHERE owner_kind = 'platform'
  AND provider_name = 'web_search.exa'
  AND json_extract(config_json, '$.arkloop_default_seed') = 'inserted'
  AND secret_id IS NULL
  AND key_prefix IS NULL
  AND base_url IS NULL;

UPDATE tool_provider_configs
SET is_active = 0,
    config_json = json_remove(config_json, '$.arkloop_default_seed'),
    updated_at = datetime('now')
WHERE owner_kind = 'platform'
  AND provider_name = 'web_search.exa'
  AND json_extract(config_json, '$.arkloop_default_seed') = 'activated';
