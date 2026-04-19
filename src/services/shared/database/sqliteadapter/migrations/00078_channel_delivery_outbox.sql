-- +goose Up

CREATE TABLE channel_delivery_outbox (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL,
    thread_id       TEXT,
    channel_id      TEXT NOT NULL,
    channel_type    TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'message',
    status          TEXT NOT NULL DEFAULT 'pending',
    payload_json    TEXT NOT NULL DEFAULT '{}',
    segments_sent   INTEGER NOT NULL DEFAULT 0,
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_retry_at   TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX idx_outbox_drain ON channel_delivery_outbox (status, next_retry_at)
    WHERE status = 'pending';
CREATE UNIQUE INDEX idx_outbox_run ON channel_delivery_outbox (run_id, kind)
    WHERE status != 'dead';

-- +goose Down

DROP INDEX IF EXISTS idx_outbox_run;
DROP INDEX IF EXISTS idx_outbox_drain;
DROP TABLE IF EXISTS channel_delivery_outbox;
