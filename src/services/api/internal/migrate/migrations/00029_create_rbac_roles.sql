-- +goose Up

CREATE TABLE rbac_roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID        REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    permissions TEXT[]      NOT NULL DEFAULT '{}',
    is_system   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE UNIQUE INDEX idx_rbac_roles_system_name ON rbac_roles(name) WHERE org_id IS NULL;

-- 内置系统角色
INSERT INTO rbac_roles (name, permissions, is_system) VALUES
(
    'platform_admin',
    ARRAY[
        'platform.admin',
        'org.members.invite', 'org.members.list', 'org.members.revoke',
        'data.threads.read', 'data.threads.write',
        'data.runs.read', 'data.runs.write',
        'data.api_keys.manage',
        'data.skills.read',
        'data.llm_credentials.manage',
        'data.mcp_configs.manage',
        'data.secrets.manage'
    ],
    TRUE
),
(
    'org_admin',
    ARRAY[
        'org.members.invite', 'org.members.list', 'org.members.revoke',
        'data.threads.read', 'data.threads.write',
        'data.runs.read', 'data.runs.write',
        'data.api_keys.manage',
        'data.skills.read',
        'data.llm_credentials.manage',
        'data.mcp_configs.manage',
        'data.secrets.manage'
    ],
    TRUE
),
(
    'org_member',
    ARRAY[
        'data.threads.read', 'data.threads.write',
        'data.runs.read', 'data.runs.write',
        'data.api_keys.manage',
        'data.skills.read'
    ],
    TRUE
);

ALTER TABLE org_memberships ADD COLUMN role_id UUID REFERENCES rbac_roles(id);

UPDATE org_memberships m
SET role_id = r.id
FROM rbac_roles r
WHERE r.name = 'org_admin'
  AND r.org_id IS NULL
  AND m.role = 'owner';

UPDATE org_memberships m
SET role_id = r.id
FROM rbac_roles r
WHERE r.name = 'org_member'
  AND r.org_id IS NULL
  AND m.role = 'member';

-- +goose Down
ALTER TABLE org_memberships DROP COLUMN IF EXISTS role_id;
DROP TABLE IF EXISTS rbac_roles;
