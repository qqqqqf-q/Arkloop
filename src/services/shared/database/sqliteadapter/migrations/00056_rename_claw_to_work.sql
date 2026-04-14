-- +goose NO TRANSACTION
-- +goose Up

PRAGMA foreign_keys = OFF;

UPDATE threads SET mode = 'work' WHERE mode = 'claw';

-- SQLite 不支持 ALTER CONSTRAINT，重建表以更新 CHECK 约束
CREATE TABLE threads_new (
    id                       TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id               TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_by_user_id       TEXT REFERENCES users(id) ON DELETE SET NULL,
    title                    TEXT,
    project_id               TEXT,
    deleted_at               TEXT,
    is_private               INTEGER NOT NULL DEFAULT 0,
    expires_at               TEXT,
    parent_thread_id         TEXT REFERENCES threads(id) ON DELETE SET NULL,
    branched_from_message_id TEXT,
    title_locked             INTEGER NOT NULL DEFAULT 0,
    mode                     TEXT NOT NULL DEFAULT 'chat' CHECK (mode IN ('chat', 'work')),
    created_at               TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (id, account_id)
);

INSERT INTO threads_new (
    id,
    account_id,
    created_by_user_id,
    title,
    project_id,
    deleted_at,
    is_private,
    expires_at,
    parent_thread_id,
    branched_from_message_id,
    title_locked,
    mode,
    created_at
)
SELECT
    id,
    account_id,
    created_by_user_id,
    title,
    project_id,
    deleted_at,
    is_private,
    expires_at,
    parent_thread_id,
    branched_from_message_id,
    title_locked,
    mode,
    created_at
FROM threads;
DROP TABLE threads;
ALTER TABLE threads_new RENAME TO threads;

CREATE INDEX ix_threads_org_id ON threads(account_id);
CREATE INDEX ix_threads_created_by_user_id ON threads(created_by_user_id);
CREATE INDEX ix_threads_deleted_at ON threads(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX idx_threads_parent_thread_id ON threads(parent_thread_id) WHERE parent_thread_id IS NOT NULL;

UPDATE feature_flags SET key = 'work_enabled', description = 'enable work mode' WHERE key = 'claw_enabled';

PRAGMA foreign_keys = ON;

-- +goose Down

PRAGMA foreign_keys = OFF;

UPDATE feature_flags SET key = 'claw_enabled', description = 'Enable Claw mode for agents' WHERE key = 'work_enabled';

UPDATE threads SET mode = 'claw' WHERE mode = 'work';

CREATE TABLE threads_new (
    id                       TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id               TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_by_user_id       TEXT REFERENCES users(id) ON DELETE SET NULL,
    title                    TEXT,
    project_id               TEXT,
    deleted_at               TEXT,
    is_private               INTEGER NOT NULL DEFAULT 0,
    expires_at               TEXT,
    parent_thread_id         TEXT REFERENCES threads(id) ON DELETE SET NULL,
    branched_from_message_id TEXT,
    title_locked             INTEGER NOT NULL DEFAULT 0,
    mode                     TEXT NOT NULL DEFAULT 'chat' CHECK (mode IN ('chat', 'claw')),
    created_at               TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (id, account_id)
);

INSERT INTO threads_new (
    id,
    account_id,
    created_by_user_id,
    title,
    project_id,
    deleted_at,
    is_private,
    expires_at,
    parent_thread_id,
    branched_from_message_id,
    title_locked,
    mode,
    created_at
)
SELECT
    id,
    account_id,
    created_by_user_id,
    title,
    project_id,
    deleted_at,
    is_private,
    expires_at,
    parent_thread_id,
    branched_from_message_id,
    title_locked,
    mode,
    created_at
FROM threads;
DROP TABLE threads;
ALTER TABLE threads_new RENAME TO threads;

CREATE INDEX ix_threads_org_id ON threads(account_id);
CREATE INDEX ix_threads_created_by_user_id ON threads(created_by_user_id);
CREATE INDEX ix_threads_deleted_at ON threads(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX idx_threads_parent_thread_id ON threads(parent_thread_id) WHERE parent_thread_id IS NOT NULL;

PRAGMA foreign_keys = ON;
