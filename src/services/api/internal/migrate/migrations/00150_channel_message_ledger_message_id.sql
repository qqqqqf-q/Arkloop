-- +goose Up
ALTER TABLE channel_message_ledger ADD COLUMN message_id UUID REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX ix_channel_message_ledger_message_id ON channel_message_ledger (message_id) WHERE message_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ix_channel_message_ledger_message_id;
ALTER TABLE channel_message_ledger DROP COLUMN IF EXISTS message_id;
