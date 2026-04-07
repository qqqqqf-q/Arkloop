-- +goose Up

CREATE TABLE run_pipeline_events (
    id          BIGSERIAL PRIMARY KEY,
    run_id      UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    account_id  UUID NOT NULL,
    middleware  TEXT NOT NULL,
    event_name  TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    fields_json JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX run_pipeline_events_run_id_idx ON run_pipeline_events(run_id);
CREATE INDEX run_pipeline_events_created_at_idx ON run_pipeline_events(created_at);

-- +goose Down

DROP INDEX IF EXISTS run_pipeline_events_created_at_idx;
DROP INDEX IF EXISTS run_pipeline_events_run_id_idx;
DROP TABLE IF EXISTS run_pipeline_events;
