-- +goose Up
ALTER TABLE jobs ADD COLUMN lease_token UUID;

-- +goose Down
ALTER TABLE jobs DROP COLUMN IF EXISTS lease_token;
