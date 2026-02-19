-- +goose Up
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID REFERENCES orgs(id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target_type TEXT,
    target_id TEXT,
    ts TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    trace_id TEXT NOT NULL,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX ix_audit_logs_trace_id ON audit_logs(trace_id);
CREATE INDEX ix_audit_logs_org_id_ts ON audit_logs(org_id, ts);

-- +goose Down
DROP INDEX IF EXISTS ix_audit_logs_org_id_ts;
DROP INDEX IF EXISTS ix_audit_logs_trace_id;
DROP TABLE IF EXISTS audit_logs;
