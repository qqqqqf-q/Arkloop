-- +goose Up

CREATE TABLE channel_group_threads (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id       UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id TEXT        NOT NULL,
    persona_id       UUID        NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id        UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_group_threads_binding UNIQUE (channel_id, platform_chat_id, persona_id),
    CONSTRAINT uq_channel_group_threads_thread UNIQUE (thread_id)
);

CREATE INDEX ix_channel_group_threads_channel_id ON channel_group_threads(channel_id);

CREATE TABLE channel_message_deliveries (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id              UUID        REFERENCES runs(id) ON DELETE SET NULL,
    thread_id           UUID        REFERENCES threads(id) ON DELETE SET NULL,
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT        NOT NULL,
    platform_message_id TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_message_deliveries UNIQUE (channel_id, platform_chat_id, platform_message_id)
);

CREATE INDEX ix_channel_message_deliveries_run_id ON channel_message_deliveries(run_id);
CREATE INDEX ix_channel_message_deliveries_thread_id ON channel_message_deliveries(thread_id);
CREATE INDEX ix_channel_message_deliveries_channel_id ON channel_message_deliveries(channel_id);

-- +goose Down

DROP INDEX IF EXISTS ix_channel_message_deliveries_channel_id;
DROP INDEX IF EXISTS ix_channel_message_deliveries_thread_id;
DROP INDEX IF EXISTS ix_channel_message_deliveries_run_id;
DROP TABLE IF EXISTS channel_message_deliveries;
DROP INDEX IF EXISTS ix_channel_group_threads_channel_id;
DROP TABLE IF EXISTS channel_group_threads;
