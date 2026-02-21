-- +goose Up
CREATE TABLE secrets (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    encrypted_value TEXT        NOT NULL,
    key_version     INT         NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,
    CONSTRAINT uq_secrets_org_name UNIQUE (org_id, name)
);

CREATE INDEX ix_secrets_org_id ON secrets(org_id);

-- +goose Down
DROP INDEX IF EXISTS ix_secrets_org_id;
DROP TABLE IF EXISTS secrets;
