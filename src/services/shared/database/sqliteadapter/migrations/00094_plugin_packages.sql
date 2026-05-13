-- +goose Up

CREATE TABLE plugin_packages (
    id                   TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id           TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    plugin_id            TEXT NOT NULL,
    version              TEXT NOT NULL,
    display_name         TEXT NOT NULL,
    description          TEXT,
    manifest_json        TEXT NOT NULL,
    settings_schema_json TEXT NOT NULL DEFAULT '{}',
    source_kind          TEXT NOT NULL DEFAULT 'manifest',
    source_uri           TEXT,
    is_active            INTEGER NOT NULL DEFAULT 1,
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, plugin_id, version)
);

CREATE INDEX idx_plugin_packages_account_active
    ON plugin_packages (account_id, is_active, plugin_id);

-- +goose Down

DROP INDEX IF EXISTS idx_plugin_packages_account_active;
DROP TABLE IF EXISTS plugin_packages;
