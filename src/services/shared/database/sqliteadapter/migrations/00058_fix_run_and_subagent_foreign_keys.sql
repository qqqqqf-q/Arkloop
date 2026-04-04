-- +goose NO TRANSACTION
-- +goose Up

PRAGMA foreign_keys = OFF;

ALTER TABLE run_events RENAME TO run_events_old_00058;
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
FROM run_events_old_00058;
DROP TABLE run_events_old_00058;
CREATE INDEX ix_run_events_type ON run_events(type);
CREATE INDEX ix_run_events_run_seq ON run_events(run_id, seq);

ALTER TABLE sub_agent_events RENAME TO sub_agent_events_old_00058;
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
FROM sub_agent_events_old_00058;
DROP TABLE sub_agent_events_old_00058;
CREATE INDEX idx_sub_agent_events_sub_agent_id_ts ON sub_agent_events(sub_agent_id, ts);
CREATE INDEX idx_sub_agent_events_type ON sub_agent_events(type);
CREATE INDEX idx_sub_agent_events_run_id ON sub_agent_events(run_id) WHERE run_id IS NOT NULL;

ALTER TABLE sub_agent_pending_inputs RENAME TO sub_agent_pending_inputs_old_00058;
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
FROM sub_agent_pending_inputs_old_00058;
DROP TABLE sub_agent_pending_inputs_old_00058;
CREATE INDEX idx_sub_agent_pending_inputs_sub_agent_id_seq
    ON sub_agent_pending_inputs(sub_agent_id, priority DESC, seq ASC);

ALTER TABLE sub_agent_context_snapshots RENAME TO sub_agent_context_snapshots_old_00058;
CREATE TABLE sub_agent_context_snapshots (
    sub_agent_id  TEXT PRIMARY KEY REFERENCES sub_agents(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO sub_agent_context_snapshots (sub_agent_id, snapshot_json, created_at, updated_at)
SELECT sub_agent_id, snapshot_json, created_at, updated_at
FROM sub_agent_context_snapshots_old_00058;
DROP TABLE sub_agent_context_snapshots_old_00058;
CREATE INDEX idx_sub_agent_context_snapshots_updated_at
    ON sub_agent_context_snapshots(updated_at);

PRAGMA foreign_keys = ON;

-- +goose Down

SELECT 1;
