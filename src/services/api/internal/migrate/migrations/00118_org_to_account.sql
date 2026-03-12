-- +goose Up

-- ============================================================
-- Phase 1: Drop org_invitations (single-user model)
-- ============================================================

DROP TABLE IF EXISTS org_invitations;

-- ============================================================
-- Phase 2: Core table renames  org -> account
-- ============================================================

ALTER TABLE orgs RENAME TO accounts;
ALTER TABLE org_memberships RENAME TO account_memberships;
ALTER TABLE org_entitlement_overrides RENAME TO account_entitlement_overrides;
ALTER TABLE org_settings RENAME TO account_settings;
ALTER TABLE org_feature_overrides RENAME TO account_feature_overrides;

-- ============================================================
-- Phase 3: org_id -> account_id column renames
-- ============================================================

-- renamed tables
ALTER TABLE account_memberships RENAME COLUMN org_id TO account_id;
ALTER TABLE account_entitlement_overrides RENAME COLUMN org_id TO account_id;
ALTER TABLE account_settings RENAME COLUMN org_id TO account_id;
ALTER TABLE account_feature_overrides RENAME COLUMN org_id TO account_id;

-- core domain
ALTER TABLE projects RENAME COLUMN org_id TO account_id;
ALTER TABLE threads RENAME COLUMN org_id TO account_id;
ALTER TABLE runs RENAME COLUMN org_id TO account_id;
ALTER TABLE messages RENAME COLUMN org_id TO account_id;

-- auth / keys
ALTER TABLE api_keys RENAME COLUMN org_id TO account_id;
ALTER TABLE rbac_roles RENAME COLUMN org_id TO account_id;
ALTER TABLE ip_rules RENAME COLUMN org_id TO account_id;

-- credentials
ALTER TABLE llm_credentials RENAME COLUMN org_id TO account_id;
ALTER TABLE llm_routes RENAME COLUMN org_id TO account_id;
ALTER TABLE asr_credentials RENAME COLUMN org_id TO account_id;
ALTER TABLE secrets RENAME COLUMN org_id TO account_id;

-- tools
ALTER TABLE tool_provider_configs RENAME COLUMN org_id TO account_id;
ALTER TABLE tool_description_overrides RENAME COLUMN org_id TO account_id;
ALTER TABLE mcp_configs RENAME COLUMN org_id TO account_id;

-- personas / skills
ALTER TABLE personas RENAME COLUMN org_id TO account_id;
ALTER TABLE skill_packages RENAME COLUMN org_id TO account_id;
ALTER TABLE profile_skill_installs RENAME COLUMN org_id TO account_id;
ALTER TABLE workspace_skill_enablements RENAME COLUMN org_id TO account_id;

-- billing
ALTER TABLE credits RENAME COLUMN org_id TO account_id;
ALTER TABLE credit_transactions RENAME COLUMN org_id TO account_id;
ALTER TABLE subscriptions RENAME COLUMN org_id TO account_id;
ALTER TABLE usage_records RENAME COLUMN org_id TO account_id;
ALTER TABLE redemption_records RENAME COLUMN org_id TO account_id;

-- infra / runtime
ALTER TABLE shell_sessions RENAME COLUMN org_id TO account_id;
ALTER TABLE default_shell_session_bindings RENAME COLUMN org_id TO account_id;
ALTER TABLE default_workspace_bindings RENAME COLUMN org_id TO account_id;
ALTER TABLE profile_registries RENAME COLUMN org_id TO account_id;
ALTER TABLE workspace_registries RENAME COLUMN org_id TO account_id;
ALTER TABLE browser_state_registries RENAME COLUMN org_id TO account_id;

-- org-scoped configs
ALTER TABLE agent_configs RENAME COLUMN org_id TO account_id;
ALTER TABLE prompt_templates RENAME COLUMN org_id TO account_id;
ALTER TABLE teams RENAME COLUMN org_id TO account_id;

-- webhooks / audit / notifications
ALTER TABLE webhook_endpoints RENAME COLUMN org_id TO account_id;
ALTER TABLE webhook_deliveries RENAME COLUMN org_id TO account_id;
ALTER TABLE audit_logs RENAME COLUMN org_id TO account_id;
ALTER TABLE notifications RENAME COLUMN org_id TO account_id;
ALTER TABLE user_memory_snapshots RENAME COLUMN org_id TO account_id;

-- ============================================================
-- Phase 4: Scope cleanup - llm_credentials
-- ============================================================

DROP INDEX IF EXISTS llm_credentials_project_name_idx;
DROP INDEX IF EXISTS llm_credentials_platform_name_idx;

ALTER TABLE llm_credentials DROP COLUMN IF EXISTS scope;

CREATE UNIQUE INDEX llm_credentials_platform_name_idx
    ON llm_credentials (name)
    WHERE owner_kind = 'platform';

-- ============================================================
-- Phase 5: Scope cleanup - secrets
-- ============================================================

DROP INDEX IF EXISTS secrets_org_name_idx;
DROP INDEX IF EXISTS secrets_platform_name_idx;

ALTER TABLE secrets DROP COLUMN IF EXISTS scope;

CREATE UNIQUE INDEX secrets_platform_name_idx
    ON secrets (name)
    WHERE owner_kind = 'platform';

-- ============================================================
-- Phase 6: Scope cleanup - asr_credentials
-- ============================================================

DROP INDEX IF EXISTS asr_credentials_org_name_idx;
DROP INDEX IF EXISTS asr_credentials_org_default_idx;
DROP INDEX IF EXISTS asr_credentials_platform_name_idx;
DROP INDEX IF EXISTS asr_credentials_platform_default_idx;

ALTER TABLE asr_credentials DROP COLUMN IF EXISTS scope;

CREATE UNIQUE INDEX asr_credentials_platform_name_idx
    ON asr_credentials (name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX asr_credentials_platform_default_idx
    ON asr_credentials (is_default)
    WHERE owner_kind = 'platform' AND is_default = true AND revoked_at IS NULL;

-- ============================================================
-- Phase 7: Scope cleanup - tool_provider_configs
-- ============================================================

ALTER TABLE tool_provider_configs
    ADD COLUMN IF NOT EXISTS owner_kind TEXT DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id);

UPDATE tool_provider_configs SET owner_kind = 'platform' WHERE scope = 'platform';

UPDATE tool_provider_configs tpc
SET owner_kind = 'user',
    owner_user_id = p.owner_user_id
FROM projects p
WHERE tpc.scope = 'project' AND tpc.project_id = p.id;

-- catch any remaining project-scoped rows without a matched project
UPDATE tool_provider_configs
SET owner_kind = 'user'
WHERE scope = 'project' AND owner_kind != 'user';

DROP INDEX IF EXISTS ix_tool_provider_configs_platform_group_active;
DROP INDEX IF EXISTS tool_provider_configs_platform_provider_idx;
DROP INDEX IF EXISTS ix_tool_provider_configs_project_group_active;
DROP INDEX IF EXISTS tool_provider_configs_project_provider_idx;

ALTER TABLE tool_provider_configs DROP COLUMN IF EXISTS scope;
ALTER TABLE tool_provider_configs DROP COLUMN IF EXISTS project_id;

ALTER TABLE tool_provider_configs DROP CONSTRAINT IF EXISTS chk_tool_provider_configs_owner_kind;
ALTER TABLE tool_provider_configs
    ADD CONSTRAINT chk_tool_provider_configs_owner_kind
        CHECK (owner_kind IN ('platform', 'user'));

CREATE UNIQUE INDEX tool_provider_configs_platform_provider_idx
    ON tool_provider_configs (provider_name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX ix_tool_provider_configs_platform_group_active
    ON tool_provider_configs (group_name)
    WHERE owner_kind = 'platform' AND is_active = true;

-- ============================================================
-- Phase 8: Scope cleanup - tool_description_overrides
-- ============================================================

DROP INDEX IF EXISTS uq_tool_description_overrides_platform_tool;
DROP INDEX IF EXISTS uq_tool_description_overrides_project_tool;

ALTER TABLE tool_description_overrides DROP COLUMN IF EXISTS scope;
ALTER TABLE tool_description_overrides DROP COLUMN IF EXISTS project_id;
ALTER TABLE tool_description_overrides DROP COLUMN IF EXISTS account_id;

CREATE UNIQUE INDEX uq_tool_description_overrides_tool
    ON tool_description_overrides (tool_name);

-- ============================================================
-- Phase 9: Index renames
-- ============================================================

-- account_memberships (was org_memberships)
ALTER INDEX ix_org_memberships_org_id RENAME TO ix_account_memberships_account_id;
ALTER INDEX ix_org_memberships_user_id RENAME TO ix_account_memberships_user_id;

-- account_entitlement_overrides
ALTER INDEX idx_org_entitlement_overrides_org_id RENAME TO idx_account_entitlement_overrides_account_id;

-- account_settings / account_feature_overrides
ALTER INDEX ix_org_settings_key RENAME TO ix_account_settings_key;
ALTER INDEX idx_org_feature_overrides_org_id RENAME TO idx_account_feature_overrides_account_id;

-- projects / threads / runs / messages
ALTER INDEX idx_projects_org_id RENAME TO idx_projects_account_id;
ALTER INDEX ix_threads_org_id RENAME TO ix_threads_account_id;
ALTER INDEX ix_runs_org_id RENAME TO ix_runs_account_id;
ALTER INDEX ix_runs_org_id_created_at_id RENAME TO ix_runs_account_id_created_at_id;
ALTER INDEX ix_messages_org_id_thread_id_created_at RENAME TO ix_messages_account_id_thread_id_created_at;

-- api_keys / credentials / secrets
ALTER INDEX idx_api_keys_org_id RENAME TO idx_api_keys_account_id;
ALTER INDEX ix_llm_credentials_org_id RENAME TO ix_llm_credentials_account_id;
ALTER INDEX ix_llm_routes_org_id RENAME TO ix_llm_routes_account_id;
ALTER INDEX ix_secrets_org_id RENAME TO ix_secrets_account_id;

-- billing
ALTER INDEX idx_credit_transactions_org_created RENAME TO idx_credit_transactions_account_created;
ALTER INDEX uq_subscriptions_org_active RENAME TO uq_subscriptions_account_active;
ALTER INDEX idx_usage_records_org_recorded RENAME TO idx_usage_records_account_recorded;

-- teams / webhooks / audit / ip_rules
ALTER INDEX idx_teams_org_id RENAME TO idx_teams_account_id;
ALTER INDEX idx_webhook_endpoints_org_id RENAME TO idx_webhook_endpoints_account_id;
ALTER INDEX idx_webhook_deliveries_org_id RENAME TO idx_webhook_deliveries_account_id;
ALTER INDEX ix_audit_logs_org_id_ts RENAME TO ix_audit_logs_account_id_ts;
ALTER INDEX idx_ip_rules_org_id RENAME TO idx_ip_rules_account_id;

-- agent_configs / prompt_templates
ALTER INDEX idx_agent_configs_org_id RENAME TO idx_agent_configs_account_id;
ALTER INDEX idx_prompt_templates_org_id RENAME TO idx_prompt_templates_account_id;

-- registries
ALTER INDEX idx_profile_registries_org_id RENAME TO idx_profile_registries_account_id;
ALTER INDEX idx_workspace_registries_org_id RENAME TO idx_workspace_registries_account_id;
ALTER INDEX idx_browser_state_registries_org_id RENAME TO idx_browser_state_registries_account_id;

-- shell_sessions
ALTER INDEX idx_shell_sessions_org_thread RENAME TO idx_shell_sessions_account_thread;
ALTER INDEX idx_shell_sessions_org_workspace RENAME TO idx_shell_sessions_account_workspace;
ALTER INDEX idx_shell_sessions_org_run RENAME TO idx_shell_sessions_account_run;
ALTER INDEX idx_shell_sessions_org_run_type RENAME TO idx_shell_sessions_account_run_type;
ALTER INDEX idx_shell_sessions_org_lease_until RENAME TO idx_shell_sessions_account_lease_until;
ALTER INDEX idx_shell_sessions_org_profile_default_binding_updated RENAME TO idx_shell_sessions_account_profile_default_binding_updated;
ALTER INDEX idx_shell_sessions_org_profile_binding_type_unique RENAME TO idx_shell_sessions_account_profile_binding_type_unique;

-- ============================================================
-- Phase 10: Named constraint renames
-- ============================================================

ALTER TABLE accounts RENAME CONSTRAINT uq_orgs_slug TO uq_accounts_slug;
ALTER TABLE account_memberships RENAME CONSTRAINT uq_org_memberships_org_id_user_id TO uq_account_memberships_account_id_user_id;
ALTER TABLE threads RENAME CONSTRAINT uq_threads_id_org_id TO uq_threads_id_account_id;
ALTER TABLE messages RENAME CONSTRAINT fk_messages_org_id_orgs TO fk_messages_account_id_accounts;
ALTER TABLE messages RENAME CONSTRAINT fk_messages_thread_org TO fk_messages_thread_account;
ALTER TABLE mcp_configs RENAME CONSTRAINT uq_mcp_configs_org_name TO uq_mcp_configs_account_name;
ALTER TABLE mcp_configs RENAME CONSTRAINT mcp_configs_org_id_fkey TO mcp_configs_account_id_fkey;
ALTER TABLE skill_packages RENAME CONSTRAINT uq_skill_packages_org_key_version TO uq_skill_packages_account_key_version;
