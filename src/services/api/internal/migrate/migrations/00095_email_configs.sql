-- +goose Up
CREATE TABLE IF NOT EXISTS email_configs (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name         text        NOT NULL,
    from_addr    text        NOT NULL DEFAULT '',
    smtp_host    text        NOT NULL DEFAULT '',
    smtp_port    text        NOT NULL DEFAULT '587',
    smtp_user    text        NOT NULL DEFAULT '',
    smtp_pass    text        NOT NULL DEFAULT '',
    smtp_tls_mode text       NOT NULL DEFAULT 'starttls',
    is_default   boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- 同一时刻只能有一个默认配置
CREATE UNIQUE INDEX IF NOT EXISTS email_configs_default_idx
    ON email_configs (is_default)
    WHERE is_default = true;

-- +goose Down
DROP TABLE IF EXISTS email_configs;
