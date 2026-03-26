-- +goose Up

CREATE TABLE IF NOT EXISTS channel_identity_links (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id TEXT NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, channel_identity_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_identity_links_channel_id
    ON channel_identity_links(channel_id);

CREATE INDEX IF NOT EXISTS idx_channel_identity_links_identity_id
    ON channel_identity_links(channel_identity_id);

-- +goose Down

DROP INDEX IF EXISTS idx_channel_identity_links_identity_id;
DROP INDEX IF EXISTS idx_channel_identity_links_channel_id;
DROP TABLE IF EXISTS channel_identity_links;
