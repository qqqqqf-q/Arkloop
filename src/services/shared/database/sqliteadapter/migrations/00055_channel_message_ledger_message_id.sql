-- +goose Up

ALTER TABLE channel_message_ledger ADD COLUMN message_id TEXT REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX idx_channel_message_ledger_message_id ON channel_message_ledger(message_id);

-- +goose Down

DROP INDEX IF EXISTS idx_channel_message_ledger_message_id;
