-- +goose Up
ALTER TABLE users ADD COLUMN tokens_invalid_before TIMESTAMP WITH TIME ZONE
    NOT NULL DEFAULT to_timestamp(0);

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS tokens_invalid_before;
