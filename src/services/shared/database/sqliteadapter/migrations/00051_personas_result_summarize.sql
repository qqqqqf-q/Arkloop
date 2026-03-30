-- +goose Up
ALTER TABLE personas ADD COLUMN result_summarize_json TEXT;

-- +goose Down
ALTER TABLE personas DROP COLUMN result_summarize_json;
