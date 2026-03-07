-- +goose Up

INSERT INTO platform_settings (key, value, updated_at)
SELECT 'limit.agent_reasoning_iterations', value, updated_at
FROM platform_settings
WHERE key = 'limit.agent_max_iterations'
ON CONFLICT (key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;

DELETE FROM platform_settings WHERE key = 'limit.agent_max_iterations';

INSERT INTO org_settings (org_id, key, value, updated_at)
SELECT org_id, 'limit.agent_reasoning_iterations', value, updated_at
FROM org_settings
WHERE key = 'limit.agent_max_iterations'
ON CONFLICT (org_id, key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;

DELETE FROM org_settings WHERE key = 'limit.agent_max_iterations';

UPDATE personas
SET budgets_json = CASE
    WHEN budgets_json ? 'reasoning_iterations' THEN budgets_json - 'max_iterations'
    ELSE jsonb_set(budgets_json - 'max_iterations', '{reasoning_iterations}', budgets_json -> 'max_iterations')
END
WHERE budgets_json ? 'max_iterations';

-- +goose Down

INSERT INTO platform_settings (key, value, updated_at)
SELECT 'limit.agent_max_iterations', value, updated_at
FROM platform_settings
WHERE key = 'limit.agent_reasoning_iterations'
ON CONFLICT (key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;

DELETE FROM platform_settings WHERE key = 'limit.agent_reasoning_iterations';

INSERT INTO org_settings (org_id, key, value, updated_at)
SELECT org_id, 'limit.agent_max_iterations', value, updated_at
FROM org_settings
WHERE key = 'limit.agent_reasoning_iterations'
ON CONFLICT (org_id, key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;

DELETE FROM org_settings WHERE key = 'limit.agent_reasoning_iterations';

UPDATE personas
SET budgets_json = CASE
    WHEN budgets_json ? 'max_iterations' THEN budgets_json - 'reasoning_iterations'
    ELSE jsonb_set(budgets_json - 'reasoning_iterations', '{max_iterations}', budgets_json -> 'reasoning_iterations')
END
WHERE budgets_json ? 'reasoning_iterations';
