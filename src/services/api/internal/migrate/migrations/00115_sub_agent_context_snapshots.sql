-- +goose Up

CREATE TABLE sub_agent_context_snapshots (
    sub_agent_id UUID PRIMARY KEY REFERENCES sub_agents(id) ON DELETE CASCADE,
    snapshot_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sub_agent_context_snapshots_updated_at
    ON sub_agent_context_snapshots(updated_at);

-- +goose Down

DROP INDEX IF EXISTS idx_sub_agent_context_snapshots_updated_at;

DROP TABLE IF EXISTS sub_agent_context_snapshots;
