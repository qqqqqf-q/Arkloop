-- +goose Up

ALTER TABLE skill_packages
    ADD COLUMN registry_provider TEXT NULL,
    ADD COLUMN registry_slug TEXT NULL,
    ADD COLUMN registry_owner_handle TEXT NULL,
    ADD COLUMN registry_version TEXT NULL,
    ADD COLUMN registry_detail_url TEXT NULL,
    ADD COLUMN registry_download_url TEXT NULL,
    ADD COLUMN registry_source_kind TEXT NULL,
    ADD COLUMN registry_source_url TEXT NULL,
    ADD COLUMN scan_status TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN scan_has_warnings BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN scan_checked_at TIMESTAMPTZ NULL,
    ADD COLUMN scan_engine TEXT NULL,
    ADD COLUMN scan_summary TEXT NULL,
    ADD COLUMN moderation_verdict TEXT NULL,
    ADD COLUMN scan_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE skill_packages
    ADD CONSTRAINT chk_skill_packages_scan_status
    CHECK (scan_status IN ('clean', 'suspicious', 'malicious', 'pending', 'error', 'unknown'));

CREATE INDEX idx_skill_packages_registry_slug
    ON skill_packages (registry_provider, registry_slug, registry_version);

-- +goose Down

DROP INDEX IF EXISTS idx_skill_packages_registry_slug;

ALTER TABLE skill_packages DROP CONSTRAINT IF EXISTS chk_skill_packages_scan_status;

ALTER TABLE skill_packages
    DROP COLUMN IF EXISTS scan_snapshot_json,
    DROP COLUMN IF EXISTS moderation_verdict,
    DROP COLUMN IF EXISTS scan_summary,
    DROP COLUMN IF EXISTS scan_engine,
    DROP COLUMN IF EXISTS scan_checked_at,
    DROP COLUMN IF EXISTS scan_has_warnings,
    DROP COLUMN IF EXISTS scan_status,
    DROP COLUMN IF EXISTS registry_source_url,
    DROP COLUMN IF EXISTS registry_source_kind,
    DROP COLUMN IF EXISTS registry_download_url,
    DROP COLUMN IF EXISTS registry_detail_url,
    DROP COLUMN IF EXISTS registry_version,
    DROP COLUMN IF EXISTS registry_owner_handle,
    DROP COLUMN IF EXISTS registry_slug,
    DROP COLUMN IF EXISTS registry_provider;
