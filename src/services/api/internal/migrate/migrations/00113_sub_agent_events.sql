-- +goose Up

CREATE TABLE sub_agent_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sub_agent_id UUID NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    run_id UUID NULL REFERENCES runs(id) ON DELETE SET NULL,
    seq BIGINT NOT NULL DEFAULT nextval('run_events_seq_global'),
    ts TIMESTAMPTZ NOT NULL DEFAULT now(),
    type TEXT NOT NULL,
    data_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_class TEXT NULL,
    CONSTRAINT uq_sub_agent_events_sub_agent_id_seq UNIQUE (sub_agent_id, seq)
);

CREATE INDEX idx_sub_agent_events_sub_agent_id_ts ON sub_agent_events(sub_agent_id, ts);
CREATE INDEX idx_sub_agent_events_type ON sub_agent_events(type);
CREATE INDEX idx_sub_agent_events_run_id ON sub_agent_events(run_id) WHERE run_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_sub_agent_events_run_id;
DROP INDEX IF EXISTS idx_sub_agent_events_type;
DROP INDEX IF EXISTS idx_sub_agent_events_sub_agent_id_ts;

DROP TABLE IF EXISTS sub_agent_events;
