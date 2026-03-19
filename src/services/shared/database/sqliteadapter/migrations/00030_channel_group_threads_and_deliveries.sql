-- +goose Up

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

CREATE INDEX idx_channel_group_threads_channel_id ON channel_group_threads(channel_id);

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

CREATE INDEX idx_channel_message_deliveries_run_id ON channel_message_deliveries(run_id);
CREATE INDEX idx_channel_message_deliveries_thread_id ON channel_message_deliveries(thread_id);
CREATE INDEX idx_channel_message_deliveries_channel_id ON channel_message_deliveries(channel_id);

-- +goose Down

DROP INDEX IF EXISTS idx_channel_message_deliveries_channel_id;
DROP INDEX IF EXISTS idx_channel_message_deliveries_thread_id;
DROP INDEX IF EXISTS idx_channel_message_deliveries_run_id;
DROP TABLE IF EXISTS channel_message_deliveries;
DROP INDEX IF EXISTS idx_channel_group_threads_channel_id;
DROP TABLE IF EXISTS channel_group_threads;
