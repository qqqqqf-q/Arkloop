-- Job queue and worker registration tables: jobs, worker_registrations

-- +goose Up

CREATE TABLE jobs (
    id           TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    job_type     TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'queued',
    available_at TEXT NOT NULL DEFAULT (datetime('now')),
    leased_until TEXT,
    lease_token  TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    worker_tags  TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX ix_jobs_job_type ON jobs(job_type);
CREATE INDEX ix_jobs_status_available_at ON jobs(status, available_at);
CREATE INDEX ix_jobs_status_leased_until ON jobs(status, leased_until);

CREATE TABLE worker_registrations (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    worker_id       TEXT NOT NULL UNIQUE,
    hostname        TEXT NOT NULL,
    version         TEXT NOT NULL DEFAULT 'unknown',
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'draining', 'dead')),
    capabilities    TEXT NOT NULL DEFAULT '[]',
    current_load    INTEGER NOT NULL DEFAULT 0,
    max_concurrency INTEGER NOT NULL DEFAULT 4,
    heartbeat_at    TEXT NOT NULL DEFAULT (datetime('now')),
    registered_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_worker_registrations_status ON worker_registrations(status);
CREATE INDEX idx_worker_registrations_heartbeat ON worker_registrations(heartbeat_at);

-- +goose Down

DROP INDEX IF EXISTS idx_worker_registrations_heartbeat;
DROP INDEX IF EXISTS idx_worker_registrations_status;
DROP TABLE IF EXISTS worker_registrations;
DROP INDEX IF EXISTS ix_jobs_status_leased_until;
DROP INDEX IF EXISTS ix_jobs_status_available_at;
DROP INDEX IF EXISTS ix_jobs_job_type;
DROP TABLE IF EXISTS jobs;
