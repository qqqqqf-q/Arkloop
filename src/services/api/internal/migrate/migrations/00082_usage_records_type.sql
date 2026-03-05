-- +goose Up

ALTER TABLE usage_records ADD COLUMN usage_type TEXT NOT NULL DEFAULT 'llm';

ALTER TABLE usage_records DROP CONSTRAINT usage_records_run_id_key;
ALTER TABLE usage_records ADD CONSTRAINT usage_records_run_id_usage_type_key UNIQUE (run_id, usage_type);

-- +goose Down

ALTER TABLE usage_records DROP CONSTRAINT usage_records_run_id_usage_type_key;
ALTER TABLE usage_records ADD CONSTRAINT usage_records_run_id_key UNIQUE (run_id);
ALTER TABLE usage_records DROP COLUMN usage_type;
