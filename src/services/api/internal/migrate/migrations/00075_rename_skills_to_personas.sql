-- +goose Up

-- rename skills table to personas
ALTER TABLE skills RENAME TO personas;
ALTER TABLE personas RENAME COLUMN skill_key TO persona_key;

-- update constraints
ALTER TABLE personas RENAME CONSTRAINT uq_skills_org_key_version TO uq_personas_org_key_version;
ALTER TABLE personas RENAME CONSTRAINT chk_skills_key_format TO chk_personas_key_format;
ALTER TABLE personas RENAME CONSTRAINT chk_skills_version_format TO chk_personas_version_format;

-- update check constraint expressions (persona_key instead of skill_key)
ALTER TABLE personas DROP CONSTRAINT chk_personas_key_format;
ALTER TABLE personas ADD CONSTRAINT chk_personas_key_format CHECK (persona_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$');

-- rename skill_id columns in related tables
ALTER TABLE runs RENAME COLUMN skill_id TO persona_id;
ALTER TABLE agent_configs RENAME COLUMN skill_id TO persona_id;

-- update RBAC permissions
UPDATE rbac_roles SET permissions = array_replace(permissions, 'data.skills.read', 'data.personas.read');

-- +goose Down

UPDATE rbac_roles SET permissions = array_replace(permissions, 'data.personas.read', 'data.skills.read');

ALTER TABLE agent_configs RENAME COLUMN persona_id TO skill_id;
ALTER TABLE runs RENAME COLUMN persona_id TO skill_id;

ALTER TABLE personas DROP CONSTRAINT chk_personas_key_format;
ALTER TABLE personas ADD CONSTRAINT chk_skills_key_format CHECK (persona_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$');

ALTER TABLE personas RENAME CONSTRAINT chk_personas_version_format TO chk_skills_version_format;
ALTER TABLE personas RENAME CONSTRAINT uq_personas_org_key_version TO uq_skills_org_key_version;

ALTER TABLE personas RENAME COLUMN persona_key TO skill_key;
ALTER TABLE personas RENAME TO skills;
