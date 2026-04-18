-- +goose Up
CREATE TABLE scheduled_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    persona_key     TEXT NOT NULL,
    prompt          TEXT NOT NULL,
    model           TEXT NOT NULL DEFAULT '',
    workspace_ref   TEXT NOT NULL DEFAULT '',
    work_dir        TEXT NOT NULL DEFAULT '',
    thread_id       UUID,
    schedule_kind   TEXT NOT NULL,
    interval_min    INT,
    daily_time      TEXT,
    monthly_day     INT,
    monthly_time    TEXT,
    timezone        TEXT NOT NULL DEFAULT 'UTC',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_by_user_id UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_scheduled_jobs_account_id ON scheduled_jobs (account_id);

ALTER TABLE scheduled_triggers
    ADD CONSTRAINT fk_scheduled_triggers_job_id
    FOREIGN KEY (job_id) REFERENCES scheduled_jobs(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE scheduled_triggers DROP CONSTRAINT IF EXISTS fk_scheduled_triggers_job_id;
DROP TABLE IF EXISTS scheduled_jobs;
