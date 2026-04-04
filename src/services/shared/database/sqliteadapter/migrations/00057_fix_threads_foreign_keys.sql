-- +goose NO TRANSACTION
-- +goose Up

PRAGMA foreign_keys = OFF;

ALTER TABLE messages RENAME TO messages_old_00057;
CREATE TABLE messages (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id          TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    account_id         TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    role               TEXT NOT NULL,
    content            TEXT NOT NULL,
    content_json       TEXT,
    metadata_json      TEXT NOT NULL DEFAULT '{}',
    hidden             INTEGER NOT NULL DEFAULT 0,
    deleted_at         TEXT,
    token_count        INTEGER,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    compacted          INTEGER NOT NULL DEFAULT 0
);
INSERT INTO messages (
    id, thread_id, account_id, created_by_user_id, role, content, content_json,
    metadata_json, hidden, deleted_at, token_count, created_at, compacted
)
SELECT
    id, thread_id, account_id, created_by_user_id, role, content, content_json,
    metadata_json, hidden, deleted_at, token_count, created_at, compacted
FROM messages_old_00057;
DROP TABLE messages_old_00057;
CREATE INDEX ix_messages_thread_id ON messages(thread_id);
CREATE INDEX ix_messages_org_id_thread_id_created_at ON messages(account_id, thread_id, created_at);
CREATE INDEX ix_messages_thread_compacted
    ON messages (thread_id, compacted)
    WHERE deleted_at IS NULL AND compacted = 1;

ALTER TABLE runs RENAME TO runs_old_00057;
CREATE TABLE runs (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id          TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id           TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_by_user_id  TEXT REFERENCES users(id) ON DELETE SET NULL,
    status              TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling', 'interrupted')),
    next_event_seq      INTEGER NOT NULL DEFAULT 1,
    parent_run_id       TEXT REFERENCES runs(id) ON DELETE SET NULL,
    resume_from_run_id  TEXT REFERENCES runs(id) ON DELETE SET NULL,
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
INSERT INTO runs (
    id, account_id, thread_id, created_by_user_id, status, next_event_seq, parent_run_id,
    resume_from_run_id, status_updated_at, completed_at, failed_at, duration_ms,
    total_input_tokens, total_output_tokens, total_cost_usd, model, persona_id,
    deleted_at, profile_ref, workspace_ref, created_at
)
SELECT
    id, account_id, thread_id, created_by_user_id, status, next_event_seq, parent_run_id,
    resume_from_run_id, status_updated_at, completed_at, failed_at, duration_ms,
    total_input_tokens, total_output_tokens, total_cost_usd, model, persona_id,
    deleted_at, profile_ref, workspace_ref, created_at
FROM runs_old_00057;
DROP TABLE runs_old_00057;
CREATE INDEX ix_runs_org_id ON runs(account_id);
CREATE INDEX ix_runs_thread_id ON runs(thread_id);

ALTER TABLE run_events RENAME TO run_events_old_00057;
CREATE TABLE run_events (
    event_id    TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    run_id      TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    ts          TEXT NOT NULL DEFAULT (datetime('now')),
    type        TEXT NOT NULL,
    data_json   TEXT NOT NULL DEFAULT '{}',
    tool_name   TEXT,
    error_class TEXT,
    UNIQUE (run_id, seq)
);
INSERT INTO run_events (event_id, run_id, seq, ts, type, data_json, tool_name, error_class)
SELECT event_id, run_id, seq, ts, type, data_json, tool_name, error_class
FROM run_events_old_00057;
DROP TABLE run_events_old_00057;
CREATE INDEX ix_run_events_type ON run_events(type);
CREATE INDEX ix_run_events_run_seq ON run_events(run_id, seq);

ALTER TABLE thread_stars RENAME TO thread_stars_old_00057;
CREATE TABLE thread_stars (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id  TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(thread_id, user_id)
);
INSERT INTO thread_stars (id, thread_id, user_id, created_at)
SELECT id, thread_id, user_id, created_at
FROM thread_stars_old_00057;
DROP TABLE thread_stars_old_00057;

ALTER TABLE thread_shares RENAME TO thread_shares_old_00057;
CREATE TABLE thread_shares (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id  TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO thread_shares (id, thread_id, token, created_by, created_at)
SELECT id, thread_id, token, created_by, created_at
FROM thread_shares_old_00057;
DROP TABLE thread_shares_old_00057;

ALTER TABLE thread_reports RENAME TO thread_reports_old_00057;
CREATE TABLE thread_reports (
    id          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id   TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    reporter_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason      TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO thread_reports (id, thread_id, reporter_id, reason, created_at)
SELECT id, thread_id, reporter_id, reason, created_at
FROM thread_reports_old_00057;
DROP TABLE thread_reports_old_00057;

ALTER TABLE channel_dm_threads RENAME TO channel_dm_threads_old_00057;
CREATE TABLE channel_dm_threads (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id TEXT NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    persona_id          TEXT NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id           TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, channel_identity_id, persona_id),
    UNIQUE (thread_id)
);
INSERT INTO channel_dm_threads (
    id, channel_id, channel_identity_id, persona_id, thread_id, created_at, updated_at
)
SELECT
    id, channel_id, channel_identity_id, persona_id, thread_id, created_at, updated_at
FROM channel_dm_threads_old_00057;
DROP TABLE channel_dm_threads_old_00057;
CREATE INDEX idx_channel_dm_threads_channel_identity ON channel_dm_threads(channel_identity_id);
CREATE INDEX idx_channel_dm_threads_channel_id ON channel_dm_threads(channel_id);

ALTER TABLE channel_group_threads RENAME TO channel_group_threads_old_00057;
CREATE TABLE channel_group_threads (
    id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id       TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id TEXT NOT NULL,
    persona_id       TEXT NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id        TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, platform_chat_id, persona_id),
    UNIQUE (thread_id)
);
INSERT INTO channel_group_threads (
    id, channel_id, platform_chat_id, persona_id, thread_id, created_at, updated_at
)
SELECT
    id, channel_id, platform_chat_id, persona_id, thread_id, created_at, updated_at
FROM channel_group_threads_old_00057;
DROP TABLE channel_group_threads_old_00057;
CREATE INDEX idx_channel_group_threads_channel_id ON channel_group_threads(channel_id);

ALTER TABLE channel_message_deliveries RENAME TO channel_message_deliveries_old_00057;
CREATE TABLE channel_message_deliveries (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    run_id              TEXT REFERENCES runs(id) ON DELETE SET NULL,
    thread_id           TEXT REFERENCES threads(id) ON DELETE SET NULL,
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT NOT NULL,
    platform_message_id TEXT NOT NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, platform_chat_id, platform_message_id)
);
INSERT INTO channel_message_deliveries (
    id, run_id, thread_id, channel_id, platform_chat_id, platform_message_id, created_at
)
SELECT
    id, run_id, thread_id, channel_id, platform_chat_id, platform_message_id, created_at
FROM channel_message_deliveries_old_00057;
DROP TABLE channel_message_deliveries_old_00057;
CREATE INDEX idx_channel_message_deliveries_run_id ON channel_message_deliveries(run_id);
CREATE INDEX idx_channel_message_deliveries_thread_id ON channel_message_deliveries(thread_id);
CREATE INDEX idx_channel_message_deliveries_channel_id ON channel_message_deliveries(channel_id);

ALTER TABLE channel_message_ledger RENAME TO channel_message_ledger_old_00057;
CREATE TABLE channel_message_ledger (
    id                         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id                 TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_type               TEXT NOT NULL,
    direction                  TEXT NOT NULL,
    thread_id                  TEXT REFERENCES threads(id) ON DELETE SET NULL,
    run_id                     TEXT REFERENCES runs(id) ON DELETE SET NULL,
    platform_conversation_id   TEXT NOT NULL,
    platform_message_id        TEXT NOT NULL,
    platform_parent_message_id TEXT,
    platform_thread_id         TEXT,
    sender_channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE SET NULL,
    metadata_json              TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    message_id                 TEXT REFERENCES messages(id) ON DELETE SET NULL,
    CHECK (direction IN ('inbound', 'outbound')),
    UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
);
INSERT INTO channel_message_ledger (
    id, channel_id, channel_type, direction, thread_id, run_id, platform_conversation_id,
    platform_message_id, platform_parent_message_id, platform_thread_id,
    sender_channel_identity_id, metadata_json, created_at, message_id
)
SELECT
    id, channel_id, channel_type, direction, thread_id, run_id, platform_conversation_id,
    platform_message_id, platform_parent_message_id, platform_thread_id,
    sender_channel_identity_id, metadata_json, created_at, message_id
FROM channel_message_ledger_old_00057;
DROP TABLE channel_message_ledger_old_00057;
CREATE INDEX idx_channel_message_ledger_channel_id ON channel_message_ledger(channel_id);
CREATE INDEX idx_channel_message_ledger_thread_id ON channel_message_ledger(thread_id);
CREATE INDEX idx_channel_message_ledger_run_id ON channel_message_ledger(run_id);
CREATE INDEX idx_channel_message_ledger_sender_identity_id ON channel_message_ledger(sender_channel_identity_id);
CREATE INDEX idx_channel_message_ledger_message_id ON channel_message_ledger(message_id);

ALTER TABLE sub_agents RENAME TO sub_agents_old_00057;
CREATE TABLE sub_agents (
    id                    TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id            TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    parent_run_id         TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    parent_thread_id      TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    root_run_id           TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    root_thread_id        TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    depth                 INTEGER NOT NULL CHECK (depth >= 0),
    role                  TEXT,
    persona_id            TEXT,
    nickname              TEXT,
    source_type           TEXT NOT NULL,
    context_mode          TEXT NOT NULL,
    status                TEXT NOT NULL CHECK (
        status IN (
            'created',
            'queued',
            'running',
            'waiting_input',
            'completed',
            'failed',
            'cancelled',
            'closed',
            'resumable'
        )
    ),
    current_run_id        TEXT REFERENCES runs(id) ON DELETE SET NULL,
    last_completed_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    last_output_ref       TEXT,
    last_error            TEXT,
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    started_at            TEXT,
    completed_at          TEXT,
    closed_at             TEXT
);
INSERT INTO sub_agents (
    id, account_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id, depth,
    role, persona_id, nickname, source_type, context_mode, status, current_run_id,
    last_completed_run_id, last_output_ref, last_error, created_at, started_at,
    completed_at, closed_at
)
SELECT
    id, account_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id, depth,
    role, persona_id, nickname, source_type, context_mode, status, current_run_id,
    last_completed_run_id, last_output_ref, last_error, created_at, started_at,
    completed_at, closed_at
FROM sub_agents_old_00057;
DROP TABLE sub_agents_old_00057;
CREATE INDEX idx_sub_agents_account_id ON sub_agents(account_id);
CREATE INDEX idx_sub_agents_parent_run_id ON sub_agents(parent_run_id);
CREATE INDEX idx_sub_agents_root_run_id ON sub_agents(root_run_id);
CREATE INDEX idx_sub_agents_current_run_id ON sub_agents(current_run_id) WHERE current_run_id IS NOT NULL;
CREATE INDEX idx_sub_agents_status ON sub_agents(status);

ALTER TABLE sub_agent_events RENAME TO sub_agent_events_old_00057;
CREATE TABLE sub_agent_events (
    event_id      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    sub_agent_id  TEXT NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    run_id        TEXT REFERENCES runs(id) ON DELETE SET NULL,
    seq           INTEGER NOT NULL,
    ts            TEXT NOT NULL DEFAULT (datetime('now')),
    type          TEXT NOT NULL,
    data_json     TEXT NOT NULL DEFAULT '{}',
    error_class   TEXT,
    UNIQUE (sub_agent_id, seq)
);
INSERT INTO sub_agent_events (event_id, sub_agent_id, run_id, seq, ts, type, data_json, error_class)
SELECT event_id, sub_agent_id, run_id, seq, ts, type, data_json, error_class
FROM sub_agent_events_old_00057;
DROP TABLE sub_agent_events_old_00057;
CREATE INDEX idx_sub_agent_events_sub_agent_id_ts ON sub_agent_events(sub_agent_id, ts);
CREATE INDEX idx_sub_agent_events_type ON sub_agent_events(type);
CREATE INDEX idx_sub_agent_events_run_id ON sub_agent_events(run_id) WHERE run_id IS NOT NULL;

ALTER TABLE sub_agent_pending_inputs RENAME TO sub_agent_pending_inputs_old_00057;
CREATE TABLE sub_agent_pending_inputs (
    id           TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    sub_agent_id TEXT NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    input        TEXT NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (sub_agent_id, seq)
);
INSERT INTO sub_agent_pending_inputs (id, sub_agent_id, seq, input, priority, created_at)
SELECT id, sub_agent_id, seq, input, priority, created_at
FROM sub_agent_pending_inputs_old_00057;
DROP TABLE sub_agent_pending_inputs_old_00057;
CREATE INDEX idx_sub_agent_pending_inputs_sub_agent_id_seq
    ON sub_agent_pending_inputs(sub_agent_id, priority DESC, seq ASC);

ALTER TABLE sub_agent_context_snapshots RENAME TO sub_agent_context_snapshots_old_00057;
CREATE TABLE sub_agent_context_snapshots (
    sub_agent_id  TEXT PRIMARY KEY REFERENCES sub_agents(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO sub_agent_context_snapshots (sub_agent_id, snapshot_json, created_at, updated_at)
SELECT sub_agent_id, snapshot_json, created_at, updated_at
FROM sub_agent_context_snapshots_old_00057;
DROP TABLE sub_agent_context_snapshots_old_00057;
CREATE INDEX idx_sub_agent_context_snapshots_updated_at
    ON sub_agent_context_snapshots(updated_at);

ALTER TABLE thread_compaction_snapshots RENAME TO thread_compaction_snapshots_old_00057;
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
INSERT INTO thread_compaction_snapshots (
    id, account_id, thread_id, summary_text, metadata_json, supersedes_snapshot_id,
    is_active, created_at
)
SELECT
    id, account_id, thread_id, summary_text, metadata_json, supersedes_snapshot_id,
    is_active, created_at
FROM thread_compaction_snapshots_old_00057;
DROP TABLE thread_compaction_snapshots_old_00057;
CREATE UNIQUE INDEX uq_thread_compaction_snapshots_active_thread
    ON thread_compaction_snapshots(thread_id)
    WHERE is_active = 1;
CREATE INDEX ix_thread_compaction_snapshots_thread_created_at
    ON thread_compaction_snapshots(thread_id, created_at DESC);

PRAGMA foreign_keys = ON;

-- +goose Down

SELECT 1;
