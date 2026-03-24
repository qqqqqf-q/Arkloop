-- +goose Up

-- users: 区分注册来源
ALTER TABLE users ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'web';

-- Channel 配置（一行 = 一个 Bot 实例）
CREATE TABLE channels (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel_type    TEXT        NOT NULL,
    persona_id      UUID        REFERENCES personas(id) ON DELETE SET NULL,
    credentials_id  UUID        REFERENCES secrets(id),
    owner_user_id   UUID        REFERENCES users(id) ON DELETE SET NULL,
    webhook_secret  TEXT,
    webhook_url     TEXT,
    is_active       BOOLEAN     NOT NULL DEFAULT FALSE,
    config_json     JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channels_account_type UNIQUE (account_id, channel_type)
);

CREATE INDEX ix_channels_account_id ON channels(account_id);

-- 跨平台统一身份主体
CREATE TABLE channel_identities (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type        TEXT        NOT NULL,
    platform_subject_id TEXT        NOT NULL,
    user_id             UUID        REFERENCES users(id) ON DELETE SET NULL,
    display_name        TEXT,
    avatar_url          TEXT,
    metadata            JSONB       NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_identities_type_subject UNIQUE (channel_type, platform_subject_id)
);

CREATE INDEX ix_channel_identities_user_id ON channel_identities(user_id);

-- 一次性绑定验证码
CREATE TABLE channel_identity_bind_codes (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    token                       TEXT        NOT NULL UNIQUE,
    issued_by_user_id           UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type                TEXT,
    used_at                     TIMESTAMPTZ,
    used_by_channel_identity_id UUID        REFERENCES channel_identities(id),
    expires_at                  TIMESTAMPTZ NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_channel_identity_bind_codes_user ON channel_identity_bind_codes(issued_by_user_id);

-- +goose Down

DROP INDEX IF EXISTS ix_channel_identity_bind_codes_user;
DROP TABLE IF EXISTS channel_identity_bind_codes;
DROP INDEX IF EXISTS ix_channel_identities_user_id;
DROP TABLE IF EXISTS channel_identities;
DROP INDEX IF EXISTS ix_channels_account_id;
DROP TABLE IF EXISTS channels;
ALTER TABLE users DROP COLUMN IF EXISTS source;
