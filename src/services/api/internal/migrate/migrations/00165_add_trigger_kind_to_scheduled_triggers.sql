-- +goose Up
ALTER TABLE scheduled_triggers
    ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT 'heartbeat';

ALTER TABLE scheduled_triggers
    ADD COLUMN job_id UUID;

CREATE UNIQUE INDEX idx_scheduled_triggers_job_id
    ON scheduled_triggers (job_id) WHERE job_id IS NOT NULL;

CREATE INDEX idx_scheduled_triggers_kind_next_fire
    ON scheduled_triggers (trigger_kind, next_fire_at);

-- +goose Down
DROP INDEX IF EXISTS idx_scheduled_triggers_kind_next_fire;
DROP INDEX IF EXISTS idx_scheduled_triggers_job_id;
ALTER TABLE scheduled_triggers DROP COLUMN IF EXISTS job_id;
ALTER TABLE scheduled_triggers DROP COLUMN IF EXISTS trigger_kind;
