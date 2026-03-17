-- +goose Up

CREATE TABLE channel_dm_threads (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id UUID        NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    persona_id          UUID        NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id           UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_dm_threads_binding UNIQUE (channel_id, channel_identity_id, persona_id),
    CONSTRAINT uq_channel_dm_threads_thread UNIQUE (thread_id)
);

CREATE INDEX ix_channel_dm_threads_channel_identity ON channel_dm_threads(channel_identity_id);
CREATE INDEX ix_channel_dm_threads_channel_id ON channel_dm_threads(channel_id);

CREATE TABLE channel_message_receipts (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT        NOT NULL,
    platform_message_id TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_message_receipts UNIQUE (channel_id, platform_chat_id, platform_message_id)
);

CREATE INDEX ix_channel_message_receipts_channel_id ON channel_message_receipts(channel_id);

-- +goose Down

DROP INDEX IF EXISTS ix_channel_message_receipts_channel_id;
DROP TABLE IF EXISTS channel_message_receipts;
DROP INDEX IF EXISTS ix_channel_dm_threads_channel_id;
DROP INDEX IF EXISTS ix_channel_dm_threads_channel_identity;
DROP TABLE IF EXISTS channel_dm_threads;
