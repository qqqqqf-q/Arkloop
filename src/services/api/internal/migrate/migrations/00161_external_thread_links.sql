-- +goose Up
CREATE TABLE external_thread_links (
    account_id UUID NOT NULL,
    thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    external_thread_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, thread_id, provider)
);

CREATE INDEX idx_external_thread_links_provider_external
    ON external_thread_links (provider, external_thread_id);

-- +goose Down
DROP INDEX IF EXISTS idx_external_thread_links_provider_external;
DROP TABLE IF EXISTS external_thread_links;
