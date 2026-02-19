-- +goose Up
CREATE TABLE threads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    title TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_threads_org_id ON threads(org_id);
CREATE INDEX ix_threads_created_by_user_id ON threads(created_by_user_id);

CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_messages_thread_id ON messages(thread_id);

CREATE TABLE runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'running',
    next_event_seq BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_runs_org_id ON runs(org_id);
CREATE INDEX ix_runs_thread_id ON runs(thread_id);

CREATE TABLE run_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq BIGINT NOT NULL,
    ts TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    type TEXT NOT NULL,
    data_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    tool_name TEXT,
    error_class TEXT,
    CONSTRAINT uq_run_events_run_id_seq UNIQUE (run_id, seq)
);

CREATE INDEX ix_run_events_type ON run_events(type);
CREATE INDEX ix_run_events_tool_name ON run_events(tool_name);
CREATE INDEX ix_run_events_error_class ON run_events(error_class);

-- +goose Down
DROP INDEX IF EXISTS ix_run_events_error_class;
DROP INDEX IF EXISTS ix_run_events_tool_name;
DROP INDEX IF EXISTS ix_run_events_type;
DROP TABLE IF EXISTS run_events;

DROP INDEX IF EXISTS ix_runs_thread_id;
DROP INDEX IF EXISTS ix_runs_org_id;
DROP TABLE IF EXISTS runs;

DROP INDEX IF EXISTS ix_messages_thread_id;
DROP TABLE IF EXISTS messages;

DROP INDEX IF EXISTS ix_threads_created_by_user_id;
DROP INDEX IF EXISTS ix_threads_org_id;
DROP TABLE IF EXISTS threads;
