-- Sub-agent tables for Desktop SQLite runtime.

-- +goose Up

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

CREATE INDEX idx_sub_agents_account_id ON sub_agents(account_id);
CREATE INDEX idx_sub_agents_parent_run_id ON sub_agents(parent_run_id);
CREATE INDEX idx_sub_agents_root_run_id ON sub_agents(root_run_id);
CREATE INDEX idx_sub_agents_current_run_id ON sub_agents(current_run_id) WHERE current_run_id IS NOT NULL;
CREATE INDEX idx_sub_agents_status ON sub_agents(status);

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

CREATE INDEX idx_sub_agent_events_sub_agent_id_ts ON sub_agent_events(sub_agent_id, ts);
CREATE INDEX idx_sub_agent_events_type ON sub_agent_events(type);
CREATE INDEX idx_sub_agent_events_run_id ON sub_agent_events(run_id) WHERE run_id IS NOT NULL;

CREATE TABLE sub_agent_pending_inputs (
    id           TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    sub_agent_id TEXT NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    input        TEXT NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (sub_agent_id, seq)
);

CREATE INDEX idx_sub_agent_pending_inputs_sub_agent_id_seq
    ON sub_agent_pending_inputs(sub_agent_id, priority DESC, seq ASC);

CREATE TABLE sub_agent_context_snapshots (
    sub_agent_id  TEXT PRIMARY KEY REFERENCES sub_agents(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_sub_agent_context_snapshots_updated_at
    ON sub_agent_context_snapshots(updated_at);

-- +goose Down

DROP INDEX IF EXISTS idx_sub_agent_context_snapshots_updated_at;
DROP TABLE IF EXISTS sub_agent_context_snapshots;

DROP INDEX IF EXISTS idx_sub_agent_pending_inputs_sub_agent_id_seq;
DROP TABLE IF EXISTS sub_agent_pending_inputs;

DROP INDEX IF EXISTS idx_sub_agent_events_run_id;
DROP INDEX IF EXISTS idx_sub_agent_events_type;
DROP INDEX IF EXISTS idx_sub_agent_events_sub_agent_id_ts;
DROP TABLE IF EXISTS sub_agent_events;

DROP INDEX IF EXISTS idx_sub_agents_status;
DROP INDEX IF EXISTS idx_sub_agents_current_run_id;
DROP INDEX IF EXISTS idx_sub_agents_root_run_id;
DROP INDEX IF EXISTS idx_sub_agents_parent_run_id;
DROP INDEX IF EXISTS idx_sub_agents_account_id;
DROP TABLE IF EXISTS sub_agents;
