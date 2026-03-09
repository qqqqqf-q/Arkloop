-- +goose Up

ALTER TABLE shell_sessions
    ADD COLUMN IF NOT EXISTS default_binding_key TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_shell_sessions_org_profile_default_binding_updated
    ON shell_sessions (org_id, profile_ref, default_binding_key, updated_at DESC)
    WHERE default_binding_key IS NOT NULL;

ALTER TABLE profile_registries
    ADD COLUMN IF NOT EXISTS owner_user_id UUID NULL,
    ADD COLUMN IF NOT EXISTS default_workspace_ref TEXT NULL,
    ADD COLUMN IF NOT EXISTS store_key TEXT NULL,
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE workspace_registries
    ADD COLUMN IF NOT EXISTS owner_user_id UUID NULL,
    ADD COLUMN IF NOT EXISTS project_id UUID NULL,
    ADD COLUMN IF NOT EXISTS default_shell_session_ref TEXT NULL,
    ADD COLUMN IF NOT EXISTS store_key TEXT NULL,
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE shell_sessions ss
   SET default_binding_key = CASE dsb.binding_scope
       WHEN 'thread' THEN 'thread:' || dsb.binding_target
       WHEN 'workspace' THEN 'workspace:' || dsb.binding_target
       ELSE NULL
   END,
       updated_at = now()
  FROM default_shell_session_bindings dsb
 WHERE ss.org_id = dsb.org_id
   AND ss.profile_ref = dsb.profile_ref
   AND ss.session_ref = dsb.session_ref
   AND ss.default_binding_key IS NULL;

DROP INDEX IF EXISTS idx_default_shell_session_bindings_session_ref;
DROP TABLE IF EXISTS default_shell_session_bindings;

-- +goose Down

CREATE TABLE IF NOT EXISTS default_shell_session_bindings (
    org_id         UUID        NOT NULL,
    profile_ref    TEXT        NOT NULL,
    binding_scope  TEXT        NOT NULL,
    binding_target TEXT        NOT NULL,
    session_ref    TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, profile_ref, binding_scope, binding_target)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_default_shell_session_bindings_session_ref
    ON default_shell_session_bindings (session_ref);

INSERT INTO default_shell_session_bindings (org_id, profile_ref, binding_scope, binding_target, session_ref)
SELECT org_id,
       profile_ref,
       CASE
           WHEN default_binding_key LIKE 'thread:%' THEN 'thread'
           WHEN default_binding_key LIKE 'workspace:%' THEN 'workspace'
           ELSE NULL
       END,
       CASE
           WHEN default_binding_key LIKE 'thread:%' THEN substring(default_binding_key FROM 8)
           WHEN default_binding_key LIKE 'workspace:%' THEN substring(default_binding_key FROM 11)
           ELSE NULL
       END,
       session_ref
  FROM shell_sessions
 WHERE default_binding_key IS NOT NULL
   AND (
       default_binding_key LIKE 'thread:%'
       OR default_binding_key LIKE 'workspace:%'
   )
ON CONFLICT (org_id, profile_ref, binding_scope, binding_target) DO UPDATE SET
    session_ref = EXCLUDED.session_ref,
    updated_at = now();

DROP INDEX IF EXISTS idx_shell_sessions_org_profile_default_binding_updated;

ALTER TABLE shell_sessions
    DROP COLUMN IF EXISTS default_binding_key;

ALTER TABLE workspace_registries
    DROP COLUMN IF EXISTS last_used_at,
    DROP COLUMN IF EXISTS store_key,
    DROP COLUMN IF EXISTS default_shell_session_ref,
    DROP COLUMN IF EXISTS project_id,
    DROP COLUMN IF EXISTS owner_user_id;

ALTER TABLE profile_registries
    DROP COLUMN IF EXISTS last_used_at,
    DROP COLUMN IF EXISTS store_key,
    DROP COLUMN IF EXISTS default_workspace_ref,
    DROP COLUMN IF EXISTS owner_user_id;
