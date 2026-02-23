-- +goose Up

CREATE TABLE plans (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT        NOT NULL UNIQUE,
    display_name TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plan_entitlements (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id    UUID NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    value_type TEXT NOT NULL CHECK (value_type IN ('int', 'bool', 'string')),
    UNIQUE (plan_id, key)
);

CREATE INDEX idx_plan_entitlements_plan_id ON plan_entitlements(plan_id);

CREATE TABLE subscriptions (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id               UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    plan_id              UUID        NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    status               TEXT        NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active', 'cancelled', 'expired')),
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end   TIMESTAMPTZ NOT NULL,
    cancelled_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_subscriptions_org_active
    ON subscriptions(org_id) WHERE status = 'active';

CREATE INDEX idx_subscriptions_plan_id ON subscriptions(plan_id);

CREATE TABLE org_entitlement_overrides (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    key                TEXT        NOT NULL,
    value              TEXT        NOT NULL,
    value_type         TEXT        NOT NULL CHECK (value_type IN ('int', 'bool', 'string')),
    reason             TEXT,
    expires_at         TIMESTAMPTZ,
    created_by_user_id UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, key)
);

CREATE INDEX idx_org_entitlement_overrides_org_id ON org_entitlement_overrides(org_id);

-- +goose Down

DROP TABLE IF EXISTS org_entitlement_overrides;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS plan_entitlements;
DROP TABLE IF EXISTS plans;
