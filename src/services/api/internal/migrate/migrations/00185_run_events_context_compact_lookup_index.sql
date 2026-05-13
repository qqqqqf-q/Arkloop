-- +goose Up

CREATE INDEX IF NOT EXISTS ix_run_events_run_type_ts_seq
    ON run_events (run_id, type, ts DESC, seq DESC);

-- +goose Down

DROP INDEX IF EXISTS ix_run_events_run_type_ts_seq;
