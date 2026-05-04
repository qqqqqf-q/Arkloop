-- +goose Up

ALTER TABLE api_keys ADD COLUMN scopes TEXT NOT NULL DEFAULT '[]';
ALTER TABLE api_keys ADD COLUMN revoked_at TEXT;

-- +goose Down

ALTER TABLE api_keys DROP COLUMN revoked_at;
ALTER TABLE api_keys DROP COLUMN scopes;
