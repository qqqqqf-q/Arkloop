-- +goose Up
CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type TEXT NOT NULL,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'queued',
    available_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    leased_until TIMESTAMP WITH TIME ZONE,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX ix_jobs_job_type ON jobs(job_type);
CREATE INDEX ix_jobs_status_available_at ON jobs(status, available_at);
CREATE INDEX ix_jobs_status_leased_until ON jobs(status, leased_until);

-- +goose Down
DROP INDEX IF EXISTS ix_jobs_status_leased_until;
DROP INDEX IF EXISTS ix_jobs_status_available_at;
DROP INDEX IF EXISTS ix_jobs_job_type;
DROP TABLE IF EXISTS jobs;
