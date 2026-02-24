-- +goose Up

CREATE TABLE asr_credentials (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider    TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    secret_id   UUID        REFERENCES secrets(id) ON DELETE SET NULL,
    key_prefix  TEXT,
    base_url    TEXT,
    model       TEXT        NOT NULL,
    is_default  BOOLEAN     NOT NULL DEFAULT false,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

-- 每个 org 最多一个 default
CREATE UNIQUE INDEX asr_credentials_org_default_idx
    ON asr_credentials (org_id)
    WHERE is_default = true AND revoked_at IS NULL;

-- +goose Down

DROP TABLE IF EXISTS asr_credentials;
