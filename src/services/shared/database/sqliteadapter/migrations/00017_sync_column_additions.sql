-- Sync column additions to match PG schema evolution.
-- Adds columns referenced by Go code but missing from SQLite tables.

-- +goose Up

-- personas: add project_id
ALTER TABLE personas ADD COLUMN project_id TEXT;

-- llm_credentials: add owner_kind, owner_user_id
ALTER TABLE llm_credentials ADD COLUMN owner_kind TEXT NOT NULL DEFAULT 'account';
ALTER TABLE llm_credentials ADD COLUMN owner_user_id TEXT;

-- llm_routes: add project_id, route_key
ALTER TABLE llm_routes ADD COLUMN project_id TEXT;
ALTER TABLE llm_routes ADD COLUMN route_key TEXT;

-- tool_provider_configs: add project_id
ALTER TABLE tool_provider_configs ADD COLUMN project_id TEXT;

-- tool_description_overrides: add project_id
ALTER TABLE tool_description_overrides ADD COLUMN project_id TEXT;

-- projects: add is_default, updated_at
ALTER TABLE projects ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN updated_at TEXT;

-- Rename org_id -> account_id where migration 00014 missed them
ALTER TABLE mcp_configs RENAME COLUMN org_id TO account_id;
ALTER TABLE personas RENAME COLUMN org_id TO account_id;
ALTER TABLE llm_credentials RENAME COLUMN org_id TO account_id;
ALTER TABLE llm_routes RENAME COLUMN org_id TO account_id;
ALTER TABLE tool_provider_configs RENAME COLUMN org_id TO account_id;
ALTER TABLE tool_description_overrides RENAME COLUMN org_id TO account_id;
ALTER TABLE projects RENAME COLUMN org_id TO account_id;

-- +goose Down

ALTER TABLE projects RENAME COLUMN account_id TO org_id;
ALTER TABLE tool_description_overrides RENAME COLUMN account_id TO org_id;
ALTER TABLE tool_provider_configs RENAME COLUMN account_id TO org_id;
ALTER TABLE llm_routes RENAME COLUMN account_id TO org_id;
ALTER TABLE llm_credentials RENAME COLUMN account_id TO org_id;
ALTER TABLE personas RENAME COLUMN account_id TO org_id;
ALTER TABLE mcp_configs RENAME COLUMN account_id TO org_id;

ALTER TABLE projects DROP COLUMN updated_at;
ALTER TABLE projects DROP COLUMN is_default;
ALTER TABLE tool_description_overrides DROP COLUMN project_id;
ALTER TABLE tool_provider_configs DROP COLUMN project_id;
ALTER TABLE llm_routes DROP COLUMN route_key;
ALTER TABLE llm_routes DROP COLUMN project_id;
ALTER TABLE llm_credentials DROP COLUMN owner_user_id;
ALTER TABLE llm_credentials DROP COLUMN owner_kind;
ALTER TABLE personas DROP COLUMN project_id;
