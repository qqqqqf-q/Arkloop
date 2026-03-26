-- +goose Up

CREATE TABLE IF NOT EXISTS channel_identity_links (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id UUID        NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_channel_identity_links UNIQUE (channel_id, channel_identity_id)
);

CREATE INDEX IF NOT EXISTS ix_channel_identity_links_channel_id
    ON channel_identity_links(channel_id);

CREATE INDEX IF NOT EXISTS ix_channel_identity_links_identity_id
    ON channel_identity_links(channel_identity_id);

-- +goose Down

DROP INDEX IF EXISTS ix_channel_identity_links_identity_id;
DROP INDEX IF EXISTS ix_channel_identity_links_channel_id;
DROP TABLE IF EXISTS channel_identity_links;
