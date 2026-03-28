-- +goose Up
CREATE TABLE thread_compaction_snapshots (
    id                     TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id             TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id              TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    summary_text           TEXT NOT NULL,
    metadata_json          TEXT NOT NULL DEFAULT '{}',
    supersedes_snapshot_id TEXT REFERENCES thread_compaction_snapshots(id) ON DELETE SET NULL,
    is_active              INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at             TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX uq_thread_compaction_snapshots_active_thread
    ON thread_compaction_snapshots(thread_id)
    WHERE is_active = 1;

CREATE INDEX ix_thread_compaction_snapshots_thread_created_at
    ON thread_compaction_snapshots(thread_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS ix_thread_compaction_snapshots_thread_created_at;
DROP INDEX IF EXISTS uq_thread_compaction_snapshots_active_thread;
DROP TABLE IF EXISTS thread_compaction_snapshots;
