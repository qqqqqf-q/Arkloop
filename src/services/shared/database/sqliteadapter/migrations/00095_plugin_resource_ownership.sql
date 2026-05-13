-- +goose Up

ALTER TABLE profile_mcp_installs ADD COLUMN owner_plugin_id TEXT;
ALTER TABLE profile_mcp_installs ADD COLUMN owner_plugin_version TEXT;

ALTER TABLE profile_skill_installs ADD COLUMN owner_plugin_id TEXT;
ALTER TABLE profile_skill_installs ADD COLUMN owner_plugin_version TEXT;

CREATE INDEX idx_profile_mcp_installs_owner_plugin
    ON profile_mcp_installs (account_id, owner_plugin_id)
    WHERE owner_plugin_id IS NOT NULL;

CREATE INDEX idx_profile_skill_installs_owner_plugin
    ON profile_skill_installs (account_id, owner_plugin_id)
    WHERE owner_plugin_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_profile_skill_installs_owner_plugin;
DROP INDEX IF EXISTS idx_profile_mcp_installs_owner_plugin;

ALTER TABLE profile_skill_installs DROP COLUMN owner_plugin_version;
ALTER TABLE profile_skill_installs DROP COLUMN owner_plugin_id;

ALTER TABLE profile_mcp_installs DROP COLUMN owner_plugin_version;
ALTER TABLE profile_mcp_installs DROP COLUMN owner_plugin_id;
