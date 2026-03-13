-- +goose Up

CREATE TABLE IF NOT EXISTS notification_broadcasts (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    type            TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    target_type     TEXT NOT NULL DEFAULT 'all',
    target_id       TEXT,
    payload_json    TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    sent_count      INTEGER NOT NULL DEFAULT 0,
    created_by      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    deleted_at      TEXT
);

ALTER TABLE notifications ADD COLUMN account_id TEXT;
ALTER TABLE notifications ADD COLUMN payload_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE notifications ADD COLUMN read_at TEXT;
ALTER TABLE notifications ADD COLUMN broadcast_id TEXT REFERENCES notification_broadcasts(id);

-- +goose Down

ALTER TABLE notifications DROP COLUMN broadcast_id;
ALTER TABLE notifications DROP COLUMN read_at;
ALTER TABLE notifications DROP COLUMN payload_json;
ALTER TABLE notifications DROP COLUMN account_id;
DROP TABLE IF EXISTS notification_broadcasts;
