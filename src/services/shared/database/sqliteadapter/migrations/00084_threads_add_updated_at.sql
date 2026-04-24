-- +goose Up
ALTER TABLE threads ADD COLUMN updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;
CREATE INDEX ix_threads_owner_activity ON threads(account_id, created_by_user_id, is_private, updated_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS ix_threads_owner_activity;
ALTER TABLE threads DROP COLUMN updated_at;
