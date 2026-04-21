-- +goose Up
CREATE TABLE account_stickers (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    content_hash TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    preview_storage_key TEXT NOT NULL DEFAULT '',
    file_size INTEGER NOT NULL DEFAULT 0,
    mime_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    is_animated INTEGER NOT NULL DEFAULT 0,
    short_tags TEXT NOT NULL DEFAULT '',
    long_desc TEXT NOT NULL DEFAULT '',
    usage_count INTEGER NOT NULL DEFAULT 0,
    last_used_at TEXT,
    is_registered INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, content_hash)
);

CREATE INDEX idx_account_stickers_hot
    ON account_stickers(account_id, usage_count DESC, last_used_at DESC)
    WHERE is_registered = 1;

CREATE INDEX idx_account_stickers_pending
    ON account_stickers(account_id, updated_at DESC)
    WHERE is_registered = 0;

CREATE TABLE sticker_description_cache (
    content_hash TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    emotion_tags TEXT NOT NULL DEFAULT '',
    timestamp TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_sticker_description_cache_timestamp
    ON sticker_description_cache(timestamp DESC);

-- +goose Down
DROP TABLE IF EXISTS sticker_description_cache;
DROP TABLE IF EXISTS account_stickers;
