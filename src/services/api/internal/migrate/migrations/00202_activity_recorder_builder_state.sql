-- +goose Up

CREATE TABLE activity_recorder_builder_state (
    account_id          UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    user_id             UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_ref         TEXT        NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    workspace_ref       TEXT        NOT NULL REFERENCES workspace_registries(workspace_ref) ON DELETE CASCADE,
    enabled             BOOLEAN     NOT NULL DEFAULT TRUE,
    interval_min        INT         NOT NULL DEFAULT 300,
    next_run_at         TIMESTAMPTZ NOT NULL,
    last_window_end_at  TIMESTAMPTZ,
    running_run_id      UUID,
    running_started_at  TIMESTAMPTZ,
    last_run_id         UUID,
    last_run_status     TEXT        NOT NULL DEFAULT '',
    last_error          TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, user_id, profile_ref, workspace_ref)
);

CREATE INDEX idx_activity_recorder_builder_state_due
    ON activity_recorder_builder_state (enabled, next_run_at);

-- +goose Down

DROP INDEX IF EXISTS idx_activity_recorder_builder_state_due;
DROP TABLE IF EXISTS activity_recorder_builder_state;
