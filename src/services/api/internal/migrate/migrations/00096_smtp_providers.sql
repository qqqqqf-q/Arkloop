-- +goose Up

CREATE TABLE smtp_providers (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    from_addr   TEXT        NOT NULL,
    smtp_host   TEXT        NOT NULL,
    smtp_port   INTEGER     NOT NULL DEFAULT 587,
    smtp_user   TEXT        NOT NULL DEFAULT '',
    smtp_pass   TEXT        NOT NULL DEFAULT '',
    tls_mode    TEXT        NOT NULL DEFAULT 'starttls'
                            CHECK (tls_mode IN ('starttls', 'tls', 'none')),
    is_default  BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 从已有 email.* platform_settings 迁移
INSERT INTO smtp_providers (name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default)
SELECT
    'Default',
    COALESCE(ps_from.value, ''),
    COALESCE(ps_host.value, ''),
    COALESCE(NULLIF(ps_port.value, '')::INTEGER, 587),
    COALESCE(ps_user.value, ''),
    COALESCE(ps_pass.value, ''),
    COALESCE(NULLIF(ps_tls.value, ''), 'starttls'),
    true
FROM platform_settings ps_from
LEFT JOIN platform_settings ps_host ON ps_host.key = 'email.smtp_host'
LEFT JOIN platform_settings ps_port ON ps_port.key = 'email.smtp_port'
LEFT JOIN platform_settings ps_user ON ps_user.key = 'email.smtp_user'
LEFT JOIN platform_settings ps_pass ON ps_pass.key = 'email.smtp_pass'
LEFT JOIN platform_settings ps_tls  ON ps_tls.key  = 'email.smtp_tls_mode'
WHERE ps_from.key = 'email.from' AND TRIM(ps_from.value) != '';

-- +goose Down

DROP TABLE IF EXISTS smtp_providers;
