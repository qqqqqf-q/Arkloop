-- +goose Up

ALTER TABLE profile_registries
    ADD COLUMN IF NOT EXISTS lease_holder_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ NULL;

ALTER TABLE profile_registries
    DROP CONSTRAINT IF EXISTS profile_registries_lease_consistency;

ALTER TABLE profile_registries
    ADD CONSTRAINT profile_registries_lease_consistency
        CHECK (
            (lease_holder_id IS NULL AND lease_until IS NULL)
            OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
        );

ALTER TABLE workspace_registries
    ADD COLUMN IF NOT EXISTS lease_holder_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ NULL;

ALTER TABLE workspace_registries
    DROP CONSTRAINT IF EXISTS workspace_registries_lease_consistency;

ALTER TABLE workspace_registries
    ADD CONSTRAINT workspace_registries_lease_consistency
        CHECK (
            (lease_holder_id IS NULL AND lease_until IS NULL)
            OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL)
        );

-- +goose Down

ALTER TABLE workspace_registries
    DROP CONSTRAINT IF EXISTS workspace_registries_lease_consistency;

ALTER TABLE workspace_registries
    DROP COLUMN IF EXISTS lease_until,
    DROP COLUMN IF EXISTS lease_holder_id;

ALTER TABLE profile_registries
    DROP CONSTRAINT IF EXISTS profile_registries_lease_consistency;

ALTER TABLE profile_registries
    DROP COLUMN IF EXISTS lease_until,
    DROP COLUMN IF EXISTS lease_holder_id;
