-- +goose Up

-- Make account_id nullable to support platform-level skills (NULL = platform-owned)
ALTER TABLE skill_packages ALTER COLUMN account_id DROP NOT NULL;

-- Sync mode for platform skill lifecycle management
ALTER TABLE skill_packages ADD COLUMN IF NOT EXISTS sync_mode TEXT NOT NULL DEFAULT 'none';
ALTER TABLE skill_packages ADD CONSTRAINT chk_skill_packages_sync_mode CHECK (sync_mode IN ('none', 'platform_skill'));

-- Content hash for change detection during sync
ALTER TABLE skill_packages ADD COLUMN IF NOT EXISTS content_hash TEXT;

-- Drop dependent foreign keys before replacing unique constraint
ALTER TABLE profile_skill_installs DROP CONSTRAINT IF EXISTS fk_profile_skill_installs_package;
ALTER TABLE workspace_skill_enablements DROP CONSTRAINT IF EXISTS fk_workspace_skill_enablements_package;

-- Replace old unique constraint with one that treats NULLs as equal
ALTER TABLE skill_packages DROP CONSTRAINT IF EXISTS uq_skill_packages_account_key_version;
CREATE UNIQUE INDEX uq_skill_packages_account_key_version ON skill_packages (account_id, skill_key, version) NULLS NOT DISTINCT;

-- Recreate foreign keys referencing the new unique index
ALTER TABLE profile_skill_installs ADD CONSTRAINT fk_profile_skill_installs_package
    FOREIGN KEY (account_id, skill_key, version) REFERENCES skill_packages(account_id, skill_key, version) ON DELETE CASCADE;
ALTER TABLE workspace_skill_enablements ADD CONSTRAINT fk_workspace_skill_enablements_package
    FOREIGN KEY (account_id, skill_key, version) REFERENCES skill_packages(account_id, skill_key, version) ON DELETE CASCADE;

-- +goose Down

-- Drop dependent foreign keys
ALTER TABLE profile_skill_installs DROP CONSTRAINT IF EXISTS fk_profile_skill_installs_package;
ALTER TABLE workspace_skill_enablements DROP CONSTRAINT IF EXISTS fk_workspace_skill_enablements_package;

DROP INDEX IF EXISTS uq_skill_packages_account_key_version;
ALTER TABLE skill_packages ADD CONSTRAINT uq_skill_packages_account_key_version UNIQUE (account_id, skill_key, version);

-- Recreate original foreign keys
ALTER TABLE profile_skill_installs ADD CONSTRAINT fk_profile_skill_installs_package
    FOREIGN KEY (account_id, skill_key, version) REFERENCES skill_packages(account_id, skill_key, version) ON DELETE CASCADE;
ALTER TABLE workspace_skill_enablements ADD CONSTRAINT fk_workspace_skill_enablements_package
    FOREIGN KEY (account_id, skill_key, version) REFERENCES skill_packages(account_id, skill_key, version) ON DELETE CASCADE;

ALTER TABLE skill_packages DROP COLUMN IF EXISTS content_hash;

ALTER TABLE skill_packages DROP CONSTRAINT IF EXISTS chk_skill_packages_sync_mode;
ALTER TABLE skill_packages DROP COLUMN IF EXISTS sync_mode;

ALTER TABLE skill_packages ALTER COLUMN account_id SET NOT NULL;
