-- Desktop local memory entries: individual text-based memory records for the
-- lightweight local memory feature (Phase 3). No vectors required.

-- +goose Up

CREATE TABLE desktop_memory_entries (
    id         TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    agent_id   TEXT NOT NULL DEFAULT 'default',
    scope      TEXT NOT NULL DEFAULT 'user',
    category   TEXT NOT NULL DEFAULT 'general',
    entry_key  TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_desktop_memory_entries_user
    ON desktop_memory_entries (account_id, user_id, agent_id);

CREATE INDEX idx_desktop_memory_entries_scope
    ON desktop_memory_entries (account_id, user_id, agent_id, scope);

-- +goose Down

DROP INDEX IF EXISTS idx_desktop_memory_entries_scope;
DROP INDEX IF EXISTS idx_desktop_memory_entries_user;
DROP TABLE IF EXISTS desktop_memory_entries;
