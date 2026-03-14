-- +goose Up

-- +goose StatementBegin
-- system_agent: 平台级服务用户，供 Platform Agent 内部调用使用，不可登录。
DO $$
DECLARE
    v_user_id    UUID;
    v_account_id UUID;
BEGIN
    -- 幂等: 如果 system_agent 已存在则跳过
    SELECT id INTO v_user_id FROM users WHERE username = 'system_agent' AND deleted_at IS NULL;
    IF v_user_id IS NOT NULL THEN
        RETURN;
    END IF;

    INSERT INTO users (username, status, is_platform_admin, created_at)
    VALUES ('system_agent', 'active', TRUE, now())
    RETURNING id INTO v_user_id;

    INSERT INTO accounts (slug, name, type, created_at)
    VALUES ('system-agent', 'System Agent', 'personal', now())
    RETURNING id INTO v_account_id;

    INSERT INTO account_memberships (account_id, user_id, role, created_at)
    VALUES (v_account_id, v_user_id, 'platform_admin', now());
END $$;
-- +goose StatementEnd

INSERT INTO rbac_roles (name, permissions, is_system)
VALUES (
    'system_agent',
    ARRAY[
        'data.personas.read', 'data.personas.manage',
        'data.skills.read', 'data.skills.manage',
        'data.llm_credentials.manage',
        'data.mcp_configs.manage',
        'data.projects.read', 'data.projects.manage',
        'data.webhooks.manage',
        'platform.feature_flags.manage'
    ],
    TRUE
)
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM rbac_roles WHERE name = 'system_agent' AND is_system = TRUE AND account_id IS NULL;

-- +goose StatementBegin
DO $$
DECLARE
    v_user_id UUID;
BEGIN
    SELECT id INTO v_user_id FROM users WHERE username = 'system_agent' AND deleted_at IS NULL;
    IF v_user_id IS NULL THEN
        RETURN;
    END IF;

    DELETE FROM account_memberships WHERE user_id = v_user_id;
    DELETE FROM accounts WHERE slug = 'system-agent' AND type = 'personal';
    DELETE FROM users WHERE id = v_user_id;
END $$;
-- +goose StatementEnd
