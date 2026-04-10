-- +goose Up
CREATE TABLE IF NOT EXISTS thread_context_replacements (
    id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id        TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    start_thread_seq INTEGER NOT NULL,
    end_thread_seq   INTEGER NOT NULL,
    summary_text     TEXT NOT NULL,
    layer            INTEGER NOT NULL DEFAULT 1,
    metadata_json    TEXT NOT NULL DEFAULT '{}',
    superseded_at    TEXT NULL,
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK (start_thread_seq <= end_thread_seq)
);

CREATE INDEX IF NOT EXISTS idx_thread_context_replacements_thread_active
    ON thread_context_replacements(thread_id, start_thread_seq, end_thread_seq, layer DESC, created_at DESC)
    WHERE superseded_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_thread_context_replacements_thread_created
    ON thread_context_replacements(thread_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_thread_context_replacements_thread_created;
DROP INDEX IF EXISTS idx_thread_context_replacements_thread_active;
DROP TABLE IF EXISTS thread_context_replacements;
