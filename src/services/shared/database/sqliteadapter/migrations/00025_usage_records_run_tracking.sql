-- +goose Up
-- Add run-tracking and cache columns to usage_records for feature parity
-- with the PostgreSQL schema. All columns are nullable so existing rows
-- are unaffected. Desktop runs will have NULL for these fields.
ALTER TABLE usage_records ADD COLUMN run_id TEXT;
ALTER TABLE usage_records ADD COLUMN input_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN output_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN cache_read_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN cache_creation_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN cached_tokens INTEGER;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; safe to no-op.
SELECT 1;
