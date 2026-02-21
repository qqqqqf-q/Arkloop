-- +goose Up
ALTER TABLE messages ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX ix_messages_hidden ON messages(hidden) WHERE hidden = TRUE;

-- +goose Down
DROP INDEX IF EXISTS ix_messages_hidden;
ALTER TABLE messages DROP COLUMN IF EXISTS hidden;
