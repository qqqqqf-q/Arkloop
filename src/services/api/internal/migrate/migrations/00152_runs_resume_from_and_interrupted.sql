-- +goose Up
ALTER TABLE runs
    ADD COLUMN resume_from_run_id UUID REFERENCES runs(id) ON DELETE SET NULL;

ALTER TABLE runs
    DROP CONSTRAINT IF EXISTS ck_runs_status;

ALTER TABLE runs
    ADD CONSTRAINT ck_runs_status
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling', 'interrupted'));

-- +goose Down
ALTER TABLE runs
    DROP CONSTRAINT IF EXISTS ck_runs_status;

ALTER TABLE runs
    ADD CONSTRAINT ck_runs_status
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling'));

ALTER TABLE runs
    DROP COLUMN IF EXISTS resume_from_run_id;
