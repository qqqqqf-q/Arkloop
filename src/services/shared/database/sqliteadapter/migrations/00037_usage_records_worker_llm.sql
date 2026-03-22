-- +goose Up
-- Worker 写入 usage_records 与 ListByThread JOIN 所需列；与 Postgres 侧语义对齐。

ALTER TABLE usage_records ADD COLUMN model TEXT NOT NULL DEFAULT '';

ALTER TABLE usage_records ADD COLUMN usage_type TEXT NOT NULL DEFAULT 'llm';

ALTER TABLE usage_records ADD COLUMN cost_usd REAL NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS usage_records_run_id_usage_type_uidx
  ON usage_records (run_id, usage_type);

-- +goose Down
SELECT 1;
