-- Add tokens_invalid_before column to users table (matches PG migration 00007).
-- Add role_id column to account_memberships table (matches PG migration 00029).

-- +goose Up

ALTER TABLE users ADD COLUMN tokens_invalid_before TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z';
ALTER TABLE account_memberships ADD COLUMN role_id TEXT;

-- +goose Down

ALTER TABLE account_memberships DROP COLUMN role_id;
ALTER TABLE users DROP COLUMN tokens_invalid_before;
