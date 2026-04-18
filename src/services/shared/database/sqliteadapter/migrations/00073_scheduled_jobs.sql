-- +goose Up

ALTER TABLE scheduled_triggers ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT 'heartbeat';
ALTER TABLE scheduled_triggers ADD COLUMN job_id TEXT;

CREATE TABLE scheduled_jobs (
    id                  TEXT PRIMARY KEY,
    account_id          TEXT NOT NULL,
    name                TEXT NOT NULL DEFAULT '',
    description         TEXT NOT NULL DEFAULT '',
    persona_key         TEXT NOT NULL DEFAULT '',
    prompt              TEXT NOT NULL DEFAULT '',
    model               TEXT NOT NULL DEFAULT '',
    workspace_ref       TEXT NOT NULL DEFAULT '',
    work_dir            TEXT NOT NULL DEFAULT '',
    thread_id           TEXT,
    schedule_kind       TEXT NOT NULL DEFAULT 'interval',
    interval_min        INTEGER,
    daily_time          TEXT NOT NULL DEFAULT '',
    monthly_day         INTEGER,
    monthly_time        TEXT NOT NULL DEFAULT '',
    timezone            TEXT NOT NULL DEFAULT 'UTC',
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_by_user_id  TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS scheduled_jobs_account_id_idx ON scheduled_jobs (account_id);
CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_job_id_uniq ON scheduled_triggers (job_id) WHERE job_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS scheduled_triggers_job_id_uniq;
DROP TABLE IF EXISTS scheduled_jobs;
-- SQLite 不支持 DROP COLUMN，忽略 trigger_kind/job_id 列的回退
