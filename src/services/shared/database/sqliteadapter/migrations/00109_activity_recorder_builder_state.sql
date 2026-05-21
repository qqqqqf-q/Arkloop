-- +goose Up

CREATE TABLE activity_recorder_builder_state (
    account_id          TEXT NOT NULL,
    user_id             TEXT NOT NULL,
    profile_ref         TEXT NOT NULL,
    workspace_ref       TEXT NOT NULL,
    enabled             INTEGER NOT NULL DEFAULT 1,
    interval_min        INTEGER NOT NULL DEFAULT 300,
    next_run_at         TEXT NOT NULL,
    last_window_end_at  TEXT,
    running_run_id      TEXT,
    running_started_at  TEXT,
    last_run_id         TEXT,
    last_run_status     TEXT NOT NULL DEFAULT '',
    last_error          TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, user_id, profile_ref, workspace_ref)
);

CREATE INDEX idx_activity_recorder_builder_state_due
    ON activity_recorder_builder_state (enabled, next_run_at);

-- +goose Down

DROP INDEX IF EXISTS idx_activity_recorder_builder_state_due;
DROP TABLE IF EXISTS activity_recorder_builder_state;
