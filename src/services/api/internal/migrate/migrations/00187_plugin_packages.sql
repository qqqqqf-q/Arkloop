-- +goose Up

CREATE TABLE plugin_packages (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id           UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    plugin_id            TEXT        NOT NULL,
    version              TEXT        NOT NULL,
    display_name         TEXT        NOT NULL,
    description          TEXT,
    manifest_json        JSONB       NOT NULL,
    settings_schema_json JSONB       NOT NULL DEFAULT '{}'::jsonb,
    source_kind          TEXT        NOT NULL DEFAULT 'manifest',
    source_uri           TEXT,
    is_active            BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_plugin_packages_account_plugin_version UNIQUE (account_id, plugin_id, version),
    CONSTRAINT chk_plugin_packages_id_format CHECK (plugin_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT chk_plugin_packages_version_format CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$')
);

CREATE INDEX idx_plugin_packages_account_active
    ON plugin_packages (account_id, is_active, plugin_id);

-- +goose Down

DROP INDEX IF EXISTS idx_plugin_packages_account_active;
DROP TABLE IF EXISTS plugin_packages;
