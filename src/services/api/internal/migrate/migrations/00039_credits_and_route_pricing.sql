-- +goose Up

ALTER TABLE llm_routes ADD COLUMN multiplier DOUBLE PRECISION NOT NULL DEFAULT 1.0;
ALTER TABLE llm_routes ADD COLUMN cost_per_1k_input DOUBLE PRECISION;
ALTER TABLE llm_routes ADD COLUMN cost_per_1k_output DOUBLE PRECISION;

CREATE TABLE credits (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID        NOT NULL UNIQUE REFERENCES orgs(id) ON DELETE CASCADE,
    balance    BIGINT      NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE credit_transactions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    amount         BIGINT      NOT NULL,
    type           TEXT        NOT NULL,
    reference_type TEXT,
    reference_id   UUID,
    note           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_credit_transactions_org_created ON credit_transactions(org_id, created_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_credit_transactions_org_created;
DROP TABLE IF EXISTS credit_transactions;
DROP TABLE IF EXISTS credits;

ALTER TABLE llm_routes DROP COLUMN IF EXISTS cost_per_1k_output;
ALTER TABLE llm_routes DROP COLUMN IF EXISTS cost_per_1k_input;
ALTER TABLE llm_routes DROP COLUMN IF EXISTS multiplier;
