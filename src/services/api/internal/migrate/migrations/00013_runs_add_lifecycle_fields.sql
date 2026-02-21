-- +goose Up
ALTER TABLE runs
    ADD COLUMN parent_run_id        UUID REFERENCES runs(id) ON DELETE SET NULL,
    ADD COLUMN status_updated_at    TIMESTAMP WITH TIME ZONE,
    ADD COLUMN completed_at         TIMESTAMP WITH TIME ZONE,
    ADD COLUMN failed_at            TIMESTAMP WITH TIME ZONE,
    ADD COLUMN duration_ms          BIGINT,
    ADD COLUMN total_input_tokens   BIGINT,
    ADD COLUMN total_output_tokens  BIGINT,
    ADD COLUMN total_cost_usd       NUMERIC(18, 8),
    ADD COLUMN model                TEXT,
    ADD COLUMN skill_id             TEXT,
    ADD COLUMN deleted_at           TIMESTAMP WITH TIME ZONE;

-- 兜底修正非法 status（00011 已处理大部分，此处防御性修正）
UPDATE runs
SET status = 'failed'
WHERE status NOT IN ('running', 'completed', 'failed', 'cancelled', 'cancelling');

ALTER TABLE runs
    ADD CONSTRAINT ck_runs_status
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling'));

-- +goose Down
ALTER TABLE runs
    DROP CONSTRAINT IF EXISTS ck_runs_status,
    DROP COLUMN IF EXISTS parent_run_id,
    DROP COLUMN IF EXISTS status_updated_at,
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS failed_at,
    DROP COLUMN IF EXISTS duration_ms,
    DROP COLUMN IF EXISTS total_input_tokens,
    DROP COLUMN IF EXISTS total_output_tokens,
    DROP COLUMN IF EXISTS total_cost_usd,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS skill_id,
    DROP COLUMN IF EXISTS deleted_at;
