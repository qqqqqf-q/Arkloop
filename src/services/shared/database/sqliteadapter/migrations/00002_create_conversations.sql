-- Conversation tables: threads, messages, runs, run_events

-- +goose Up

CREATE TABLE threads (
    id                       TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id                   TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
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
    UNIQUE (id, org_id)
);

CREATE INDEX ix_threads_org_id ON threads(org_id);
CREATE INDEX ix_threads_created_by_user_id ON threads(created_by_user_id);
CREATE INDEX ix_threads_deleted_at ON threads(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX idx_threads_parent_thread_id ON threads(parent_thread_id) WHERE parent_thread_id IS NOT NULL;

CREATE TABLE messages (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id          TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    org_id             TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    role               TEXT NOT NULL,
    content            TEXT NOT NULL,
    content_json       TEXT,
    metadata_json      TEXT NOT NULL DEFAULT '{}',
    hidden             INTEGER NOT NULL DEFAULT 0,
    deleted_at         TEXT,
    token_count        INTEGER,
    created_at         TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX ix_messages_thread_id ON messages(thread_id);
CREATE INDEX ix_messages_org_id_thread_id_created_at ON messages(org_id, thread_id, created_at);

CREATE TABLE runs (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id              TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    thread_id           TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_by_user_id  TEXT REFERENCES users(id) ON DELETE SET NULL,
    status              TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling')),
    next_event_seq      INTEGER NOT NULL DEFAULT 1,
    parent_run_id       TEXT REFERENCES runs(id) ON DELETE SET NULL,
    status_updated_at   TEXT,
    completed_at        TEXT,
    failed_at           TEXT,
    duration_ms         INTEGER,
    total_input_tokens  INTEGER,
    total_output_tokens INTEGER,
    total_cost_usd      TEXT,
    model               TEXT,
    persona_id          TEXT,
    deleted_at          TEXT,
    profile_ref         TEXT,
    workspace_ref       TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX ix_runs_org_id ON runs(org_id);
CREATE INDEX ix_runs_thread_id ON runs(thread_id);

CREATE TABLE run_events (
    event_id  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    run_id    TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq       INTEGER NOT NULL,
    ts        TEXT NOT NULL DEFAULT (datetime('now')),
    type      TEXT NOT NULL,
    data_json TEXT NOT NULL DEFAULT '{}',
    tool_name TEXT,
    error_class TEXT,
    UNIQUE (run_id, seq)
);

CREATE INDEX ix_run_events_type ON run_events(type);
CREATE INDEX ix_run_events_run_seq ON run_events(run_id, seq);

-- +goose Down

DROP INDEX IF EXISTS ix_run_events_run_seq;
DROP INDEX IF EXISTS ix_run_events_type;
DROP TABLE IF EXISTS run_events;
DROP INDEX IF EXISTS ix_runs_thread_id;
DROP INDEX IF EXISTS ix_runs_org_id;
DROP TABLE IF EXISTS runs;
DROP INDEX IF EXISTS ix_messages_org_id_thread_id_created_at;
DROP INDEX IF EXISTS ix_messages_thread_id;
DROP TABLE IF EXISTS messages;
DROP INDEX IF EXISTS idx_threads_parent_thread_id;
DROP INDEX IF EXISTS ix_threads_deleted_at;
DROP INDEX IF EXISTS ix_threads_created_by_user_id;
DROP INDEX IF EXISTS ix_threads_org_id;
DROP TABLE IF EXISTS threads;
