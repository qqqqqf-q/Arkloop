-- Memory and skill package tables: user_memory_snapshots, skill_packages,
-- profile_skill_installs, workspace_skill_enablements

-- +goose Up

CREATE TABLE user_memory_snapshots (
    org_id       TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    agent_id     TEXT NOT NULL DEFAULT 'default',
    memory_block TEXT NOT NULL,
    hits_json    TEXT,
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (org_id, user_id, agent_id)
);

CREATE TABLE skill_packages (
    id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id           TEXT NOT NULL,
    skill_key        TEXT NOT NULL,
    version          TEXT NOT NULL,
    display_name     TEXT NOT NULL,
    description      TEXT,
    instruction_path TEXT NOT NULL,
    manifest_key     TEXT NOT NULL,
    bundle_key       TEXT NOT NULL,
    files_prefix     TEXT NOT NULL,
    platforms        TEXT NOT NULL DEFAULT '[]',
    is_active        INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (org_id, skill_key, version)
);

CREATE TABLE profile_skill_installs (
    profile_ref   TEXT NOT NULL,
    org_id        TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,
    skill_key     TEXT NOT NULL,
    version       TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (profile_ref, skill_key, version)
);

CREATE INDEX idx_profile_skill_installs_profile_ref
    ON profile_skill_installs (org_id, profile_ref);

CREATE TABLE workspace_skill_enablements (
    workspace_ref      TEXT NOT NULL,
    org_id             TEXT NOT NULL,
    enabled_by_user_id TEXT NOT NULL,
    skill_key          TEXT NOT NULL,
    version            TEXT NOT NULL,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (workspace_ref, skill_key)
);

CREATE INDEX idx_workspace_skill_enablements_workspace_ref
    ON workspace_skill_enablements (org_id, workspace_ref);

-- +goose Down

DROP INDEX IF EXISTS idx_workspace_skill_enablements_workspace_ref;
DROP TABLE IF EXISTS workspace_skill_enablements;
DROP INDEX IF EXISTS idx_profile_skill_installs_profile_ref;
DROP TABLE IF EXISTS profile_skill_installs;
DROP TABLE IF EXISTS skill_packages;
DROP TABLE IF EXISTS user_memory_snapshots;
