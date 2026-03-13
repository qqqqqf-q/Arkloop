-- +goose Up

CREATE TABLE sub_agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    parent_run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    parent_thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    root_run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    root_thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    depth INTEGER NOT NULL,
    role TEXT NULL,
    persona_id TEXT NULL,
    nickname TEXT NULL,
    source_type TEXT NOT NULL,
    context_mode TEXT NOT NULL,
    status TEXT NOT NULL,
    current_run_id UUID NULL REFERENCES runs(id) ON DELETE SET NULL,
    last_completed_run_id UUID NULL REFERENCES runs(id) ON DELETE SET NULL,
    last_output_ref TEXT NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    closed_at TIMESTAMPTZ NULL,
    CONSTRAINT chk_sub_agents_status CHECK (
        status IN (
            'created',
            'queued',
            'running',
            'waiting_input',
            'completed',
            'failed',
            'cancelled',
            'closed',
            'resumable'
        )
    )
);

CREATE INDEX idx_sub_agents_org_id ON sub_agents(org_id);
CREATE INDEX idx_sub_agents_parent_run_id ON sub_agents(parent_run_id);
CREATE INDEX idx_sub_agents_root_run_id ON sub_agents(root_run_id);
CREATE INDEX idx_sub_agents_current_run_id ON sub_agents(current_run_id) WHERE current_run_id IS NOT NULL;
CREATE INDEX idx_sub_agents_status ON sub_agents(status);

-- +goose Down

DROP INDEX IF EXISTS idx_sub_agents_status;
DROP INDEX IF EXISTS idx_sub_agents_current_run_id;
DROP INDEX IF EXISTS idx_sub_agents_root_run_id;
DROP INDEX IF EXISTS idx_sub_agents_parent_run_id;
DROP INDEX IF EXISTS idx_sub_agents_org_id;

DROP TABLE IF EXISTS sub_agents;
