-- +goose Up
-- personas 新增 image_model 字段（图像理解专用，与 model 平级）。
-- 解析顺序：persona.image_model → spawn.profile.vision entitlement → fail。

ALTER TABLE personas
    ADD COLUMN image_model TEXT;

-- +goose Down

ALTER TABLE personas
    DROP COLUMN IF EXISTS image_model;
