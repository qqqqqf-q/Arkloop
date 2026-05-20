-- +goose Up
-- 移除 llm_routes.is_default。
-- 历史包袱：is_default 用于"无显式选择器时回退到默认路由"，但这导致用户被路由到他们没设置的模型，
-- 并被计费。现在显式失败：persona 没设 model 也没 spawn.profile.task override 的 run 直接 deny。
-- 工具模型与图像理解模型分别走 spawn.profile.tool / spawn.profile.vision entitlement。

DROP INDEX IF EXISTS ux_llm_routes_credential_default;

ALTER TABLE llm_routes
    DROP COLUMN IF EXISTS is_default;

-- profile "image" → "vision" 改名后，旧 key 残留需迁移
UPDATE account_entitlement_overrides SET key = 'spawn.profile.vision' WHERE key = 'image_generative.model';

-- +goose Down

-- 反向：回退 vision → image
UPDATE account_entitlement_overrides SET key = 'image_generative.model' WHERE key = 'spawn.profile.vision';

ALTER TABLE llm_routes
    ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX ux_llm_routes_credential_default
    ON llm_routes (credential_id)
    WHERE is_default = TRUE;
