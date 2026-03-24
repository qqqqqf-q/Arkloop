-- Align desktop SQLite schema with PG channel integration migrations 00130/00131.

-- +goose Up

ALTER TABLE users ADD COLUMN source TEXT NOT NULL DEFAULT 'web';

CREATE TABLE channels (
    id             TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel_type   TEXT NOT NULL,
    persona_id     TEXT REFERENCES personas(id) ON DELETE SET NULL,
    credentials_id TEXT REFERENCES secrets(id),
    owner_user_id  TEXT REFERENCES users(id),
    webhook_secret TEXT,
    webhook_url    TEXT,
    is_active      INTEGER NOT NULL DEFAULT 0,
    config_json    TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, channel_type)
);

CREATE INDEX idx_channels_account_id ON channels(account_id);

CREATE TABLE channel_identities (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_type        TEXT NOT NULL,
    platform_subject_id TEXT NOT NULL,
    user_id             TEXT REFERENCES users(id) ON DELETE SET NULL,
    display_name        TEXT,
    avatar_url          TEXT,
    metadata            TEXT NOT NULL DEFAULT '{}',
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_type, platform_subject_id)
);

CREATE INDEX idx_channel_identities_user_id ON channel_identities(user_id);

CREATE TABLE channel_identity_bind_codes (
    id                          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    token                       TEXT NOT NULL UNIQUE,
    issued_by_user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type                TEXT,
    used_at                     TEXT,
    used_by_channel_identity_id TEXT REFERENCES channel_identities(id),
    expires_at                  TEXT NOT NULL,
    created_at                  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_channel_identity_bind_codes_user ON channel_identity_bind_codes(issued_by_user_id);

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

CREATE INDEX idx_channel_dm_threads_channel_identity ON channel_dm_threads(channel_identity_id);
CREATE INDEX idx_channel_dm_threads_channel_id ON channel_dm_threads(channel_id);

CREATE TABLE channel_message_receipts (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT NOT NULL,
    platform_message_id TEXT NOT NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, platform_chat_id, platform_message_id)
);

CREATE INDEX idx_channel_message_receipts_channel_id ON channel_message_receipts(channel_id);

-- +goose Down

DROP INDEX IF EXISTS idx_channel_message_receipts_channel_id;
DROP TABLE IF EXISTS channel_message_receipts;
DROP INDEX IF EXISTS idx_channel_dm_threads_channel_id;
DROP INDEX IF EXISTS idx_channel_dm_threads_channel_identity;
DROP TABLE IF EXISTS channel_dm_threads;
DROP INDEX IF EXISTS idx_channel_identity_bind_codes_user;
DROP TABLE IF EXISTS channel_identity_bind_codes;
DROP INDEX IF EXISTS idx_channel_identities_user_id;
DROP TABLE IF EXISTS channel_identities;
DROP INDEX IF EXISTS idx_channels_account_id;
DROP TABLE IF EXISTS channels;
ALTER TABLE users DROP COLUMN source;
