-- Align skill_packages with PG migrations 00126/00127/00113:
--   • Make account_id nullable (platform skills use NULL)
--   • Add registry, scan, sync_mode, content_hash columns
--   • Add partial unique indexes for NULL / non-NULL account_id
--   • Create profile_platform_skill_overrides table

-- +goose Up

-- Recreate skill_packages with nullable account_id and all required columns.
-- SQLite cannot ALTER a column to remove NOT NULL, so we use the rename approach.

CREATE TABLE skill_packages_v2 (
    id                    TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id            TEXT,                                  -- NULL = platform-owned
    skill_key             TEXT    NOT NULL,
    version               TEXT    NOT NULL,
    display_name          TEXT    NOT NULL,
    description           TEXT,
    instruction_path      TEXT    NOT NULL DEFAULT '',
    manifest_key          TEXT    NOT NULL DEFAULT '',
    bundle_key            TEXT    NOT NULL DEFAULT '',
    files_prefix          TEXT    NOT NULL DEFAULT '',
    platforms             TEXT    NOT NULL DEFAULT '[]',
    registry_provider     TEXT,
    registry_slug         TEXT,
    registry_owner_handle TEXT,
    registry_version      TEXT,
    registry_detail_url   TEXT,
    registry_download_url TEXT,
    registry_source_kind  TEXT,
    registry_source_url   TEXT,
    scan_status           TEXT    NOT NULL DEFAULT 'unknown',
    scan_has_warnings     INTEGER NOT NULL DEFAULT 0,
    scan_checked_at       TEXT,
    scan_engine           TEXT,
    scan_summary          TEXT,
    moderation_verdict    TEXT,
    scan_snapshot_json    TEXT,
    sync_mode             TEXT    NOT NULL DEFAULT 'none',
    content_hash          TEXT,
    is_active             INTEGER NOT NULL DEFAULT 1,
    created_at            TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at            TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Copy existing rows (only columns that exist in the old table)
INSERT INTO skill_packages_v2 (
    id, account_id, skill_key, version, display_name, description,
    instruction_path, manifest_key, bundle_key, files_prefix, platforms,
    is_active, created_at, updated_at
)
SELECT
    id, account_id, skill_key, version, display_name, description,
    instruction_path, manifest_key, bundle_key, files_prefix, platforms,
    is_active, created_at, updated_at
FROM skill_packages;

DROP TABLE skill_packages;
ALTER TABLE skill_packages_v2 RENAME TO skill_packages;

-- Partial unique index for platform skills (NULL account_id).
-- Matches the ON CONFLICT (skill_key, version) WHERE account_id IS NULL
-- rewrite performed by sqlitepgx/compat.go.
CREATE UNIQUE INDEX uq_platform_skills
    ON skill_packages (skill_key, version)
    WHERE account_id IS NULL;

-- Regular unique index for user/account-owned skills.
CREATE UNIQUE INDEX uq_user_skills
    ON skill_packages (account_id, skill_key, version)
    WHERE account_id IS NOT NULL;

-- Platform skill override status per-profile (matches PG migration 00127).
CREATE TABLE IF NOT EXISTS profile_platform_skill_overrides (
    profile_ref TEXT    NOT NULL,
    skill_key   TEXT    NOT NULL,
    version     TEXT    NOT NULL,
    status      TEXT    NOT NULL DEFAULT 'manual',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (profile_ref, skill_key, version),
    CHECK (status IN ('manual', 'removed'))
);

CREATE INDEX idx_platform_skill_overrides_profile
    ON profile_platform_skill_overrides (profile_ref);

-- +goose Down

DROP INDEX IF EXISTS idx_platform_skill_overrides_profile;
DROP TABLE IF EXISTS profile_platform_skill_overrides;

-- Restore original skill_packages (lossy — new columns are dropped)
CREATE TABLE skill_packages_old (
    id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id       TEXT NOT NULL,
    skill_key        TEXT NOT NULL,
    version          TEXT NOT NULL,
    display_name     TEXT NOT NULL,
    description      TEXT,
    instruction_path TEXT NOT NULL DEFAULT '',
    manifest_key     TEXT NOT NULL DEFAULT '',
    bundle_key       TEXT NOT NULL DEFAULT '',
    files_prefix     TEXT NOT NULL DEFAULT '',
    platforms        TEXT NOT NULL DEFAULT '[]',
    is_active        INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, skill_key, version)
);

INSERT INTO skill_packages_old (
    id, account_id, skill_key, version, display_name, description,
    instruction_path, manifest_key, bundle_key, files_prefix, platforms,
    is_active, created_at, updated_at
)
SELECT
    id, COALESCE(account_id, ''), skill_key, version, display_name, description,
    instruction_path, manifest_key, bundle_key, files_prefix, platforms,
    is_active, created_at, updated_at
FROM skill_packages;

DROP INDEX IF EXISTS uq_user_skills;
DROP INDEX IF EXISTS uq_platform_skills;
DROP TABLE skill_packages;
ALTER TABLE skill_packages_old RENAME TO skill_packages;
