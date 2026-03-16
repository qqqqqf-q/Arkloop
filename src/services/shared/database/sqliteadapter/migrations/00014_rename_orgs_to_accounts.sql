-- Rename orgs -> accounts and org_memberships -> account_memberships
-- to align with the PG schema rename (OrgID -> AccountID).

-- +goose Up

ALTER TABLE orgs RENAME TO accounts;
ALTER TABLE org_memberships RENAME TO account_memberships;
ALTER TABLE account_memberships RENAME COLUMN org_id TO account_id;

-- Update columns in tables that reference org_id
ALTER TABLE threads RENAME COLUMN org_id TO account_id;
ALTER TABLE messages RENAME COLUMN org_id TO account_id;
ALTER TABLE runs RENAME COLUMN org_id TO account_id;
ALTER TABLE shell_sessions RENAME COLUMN org_id TO account_id;
ALTER TABLE default_workspace_bindings RENAME COLUMN org_id TO account_id;
ALTER TABLE profile_registries RENAME COLUMN org_id TO account_id;
ALTER TABLE workspace_registries RENAME COLUMN org_id TO account_id;
ALTER TABLE browser_state_registries RENAME COLUMN org_id TO account_id;
ALTER TABLE user_memory_snapshots RENAME COLUMN org_id TO account_id;
ALTER TABLE skill_packages RENAME COLUMN org_id TO account_id;
ALTER TABLE profile_skill_installs RENAME COLUMN org_id TO account_id;
ALTER TABLE workspace_skill_enablements RENAME COLUMN org_id TO account_id;

-- +goose Down

ALTER TABLE workspace_skill_enablements RENAME COLUMN account_id TO org_id;
ALTER TABLE profile_skill_installs RENAME COLUMN account_id TO org_id;
ALTER TABLE skill_packages RENAME COLUMN account_id TO org_id;
ALTER TABLE user_memory_snapshots RENAME COLUMN account_id TO org_id;
ALTER TABLE browser_state_registries RENAME COLUMN account_id TO org_id;
ALTER TABLE workspace_registries RENAME COLUMN account_id TO org_id;
ALTER TABLE profile_registries RENAME COLUMN account_id TO org_id;
ALTER TABLE default_workspace_bindings RENAME COLUMN account_id TO org_id;
ALTER TABLE shell_sessions RENAME COLUMN account_id TO org_id;
ALTER TABLE runs RENAME COLUMN account_id TO org_id;
ALTER TABLE messages RENAME COLUMN account_id TO org_id;
ALTER TABLE threads RENAME COLUMN account_id TO org_id;
ALTER TABLE account_memberships RENAME COLUMN account_id TO org_id;
ALTER TABLE account_memberships RENAME TO org_memberships;
ALTER TABLE accounts RENAME TO orgs;
