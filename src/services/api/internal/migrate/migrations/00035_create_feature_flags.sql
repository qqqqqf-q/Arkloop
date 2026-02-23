-- +goose Up

CREATE TABLE feature_flags (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    key           TEXT        NOT NULL UNIQUE,
    description   TEXT,
    default_value BOOLEAN     NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE org_feature_overrides (
    org_id     UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    flag_key   TEXT        NOT NULL,
    enabled    BOOLEAN     NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, flag_key)
);

CREATE INDEX idx_org_feature_overrides_org_id ON org_feature_overrides(org_id);

-- +goose Down

DROP TABLE IF EXISTS org_feature_overrides;
DROP TABLE IF EXISTS feature_flags;
