-- +goose Up
CREATE INDEX idx_runs_parent_run_id ON runs(parent_run_id) WHERE parent_run_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_runs_parent_run_id;
