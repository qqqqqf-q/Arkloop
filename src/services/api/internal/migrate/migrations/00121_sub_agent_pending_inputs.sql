-- +goose Up

CREATE TABLE sub_agent_pending_inputs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sub_agent_id UUID NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    seq BIGINT NOT NULL DEFAULT nextval('run_events_seq_global'),
    input TEXT NOT NULL,
    priority BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_sub_agent_pending_inputs_sub_agent_id_seq UNIQUE (sub_agent_id, seq)
);

CREATE INDEX idx_sub_agent_pending_inputs_sub_agent_id_seq
    ON sub_agent_pending_inputs(sub_agent_id, priority DESC, seq ASC);

-- +goose Down

DROP INDEX IF EXISTS idx_sub_agent_pending_inputs_sub_agent_id_seq;

DROP TABLE IF EXISTS sub_agent_pending_inputs;
