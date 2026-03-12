-- +goose Up

-- 将残留的 signing_secret 迁移到 secrets 表，然后删除 signing_secret 列。
-- backfillWebhookSecrets 运行时代码已在启动时处理过绝大多数行，
-- 此 migration 作为最终兜底，确保列可以安全删除。

-- 为尚未迁移的行创建 secret 记录并回填 secret_id
-- +goose StatementBegin
DO $$
DECLARE
    rec RECORD;
    new_secret_id UUID;
BEGIN
    FOR rec IN
        SELECT id, org_id, signing_secret
        FROM webhook_endpoints
        WHERE signing_secret IS NOT NULL AND secret_id IS NULL
    LOOP
        new_secret_id := gen_random_uuid();
        INSERT INTO secrets (id, org_id, name, encrypted_value, key_version)
        VALUES (new_secret_id, rec.org_id, 'webhook_endpoint:' || rec.id::text, rec.signing_secret, 1);
        UPDATE webhook_endpoints SET secret_id = new_secret_id WHERE id = rec.id;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE webhook_endpoints DROP COLUMN signing_secret;

-- +goose Down

ALTER TABLE webhook_endpoints ADD COLUMN signing_secret TEXT;
