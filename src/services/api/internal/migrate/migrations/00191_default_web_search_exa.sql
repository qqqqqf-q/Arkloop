-- +goose Up

UPDATE tool_provider_configs
SET is_active = TRUE,
    config_json = jsonb_set(COALESCE(config_json, '{}'::jsonb), '{arkloop_default_seed}', '"activated"', true),
    updated_at = now()
WHERE owner_kind = 'platform'
  AND provider_name = 'web_search.exa'
  AND NOT EXISTS (
      SELECT 1
      FROM tool_provider_configs active
      WHERE active.owner_kind = 'platform'
        AND active.group_name = 'web_search'
        AND active.is_active = TRUE
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
    TRUE,
    NULL,
    NULL,
    NULL,
    '{"arkloop_default_seed":"inserted"}'::jsonb
WHERE NOT EXISTS (
      SELECT 1
      FROM tool_provider_configs active
      WHERE active.owner_kind = 'platform'
        AND active.group_name = 'web_search'
        AND active.is_active = TRUE
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
  AND config_json->>'arkloop_default_seed' = 'inserted'
  AND secret_id IS NULL
  AND key_prefix IS NULL
  AND base_url IS NULL;

UPDATE tool_provider_configs
SET is_active = FALSE,
    config_json = config_json - 'arkloop_default_seed',
    updated_at = now()
WHERE owner_kind = 'platform'
  AND provider_name = 'web_search.exa'
  AND config_json->>'arkloop_default_seed' = 'activated';
