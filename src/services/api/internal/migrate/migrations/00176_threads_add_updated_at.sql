-- +goose Up
ALTER TABLE threads ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now();
CREATE INDEX ix_threads_owner_activity ON threads(account_id, created_by_user_id, is_private, updated_at DESC, id DESC) WHERE deleted_at IS NULL;
