-- +goose NO TRANSACTION

-- +goose Up
-- 加速 stale run reaper 的 WHERE status='running' 扫描（R73）
CREATE INDEX CONCURRENTLY ix_runs_status_running_activity
    ON runs (COALESCE(status_updated_at, created_at))
    WHERE status = 'running';

-- +goose Down
DROP INDEX IF EXISTS ix_runs_status_running_activity;
