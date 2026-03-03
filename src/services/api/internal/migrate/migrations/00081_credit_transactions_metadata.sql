-- +goose Up

ALTER TABLE credit_transactions ADD COLUMN metadata JSONB;

-- +goose Down

ALTER TABLE credit_transactions DROP COLUMN IF EXISTS metadata;
