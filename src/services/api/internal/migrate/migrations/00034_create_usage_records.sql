-- +goose Up

CREATE TABLE usage_records (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    run_id         UUID        NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,
    model          TEXT        NOT NULL DEFAULT '',
    input_tokens   BIGINT      NOT NULL DEFAULT 0,
    output_tokens  BIGINT      NOT NULL DEFAULT 0,
    cost_usd       NUMERIC(18, 8) NOT NULL DEFAULT 0,
    recorded_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_usage_records_org_recorded ON usage_records(org_id, recorded_at);

-- +goose Down

DROP TABLE IF EXISTS usage_records;
