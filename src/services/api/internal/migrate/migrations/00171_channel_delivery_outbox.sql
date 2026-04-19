-- +goose Up

CREATE TABLE channel_delivery_outbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL,
    thread_id       UUID,
    channel_id      UUID NOT NULL,
    channel_type    TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'message', -- message / injection_block_notice
    status          TEXT NOT NULL DEFAULT 'pending', -- pending / sent / dead
    payload_json    JSONB NOT NULL,
    segments_sent   INTEGER NOT NULL DEFAULT 0,
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_retry_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_outbox_drain ON channel_delivery_outbox (status, next_retry_at)
    WHERE status = 'pending';
CREATE UNIQUE INDEX idx_outbox_run ON channel_delivery_outbox (run_id, kind)
    WHERE status != 'dead';

-- +goose Down

DROP INDEX IF EXISTS idx_outbox_run;
DROP INDEX IF EXISTS idx_outbox_drain;
DROP TABLE IF EXISTS channel_delivery_outbox;
