-- +goose Up
-- llm_routes.is_default 语义已移除：不再有 default route fallback。
UPDATE llm_routes SET is_default = 0 WHERE is_default != 0;
DROP INDEX IF EXISTS ux_llm_routes_credential_default;
ALTER TABLE llm_routes DROP COLUMN is_default;

-- profile "image" → "vision" 改名后，旧 key 残留需迁移
UPDATE account_entitlement_overrides SET key = 'spawn.profile.vision' WHERE key = 'image_generative.model';

-- +goose Down

-- 反向：回退 vision → image
UPDATE account_entitlement_overrides SET key = 'image_generative.model' WHERE key = 'spawn.profile.vision';

ALTER TABLE llm_routes ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX ux_llm_routes_credential_default ON llm_routes (credential_id) WHERE is_default = 1;
