-- +goose Up

CREATE INDEX IF NOT EXISTS ix_channel_message_ledger_created_at
    ON channel_message_ledger(created_at);

-- +goose Down

DROP INDEX IF EXISTS ix_channel_message_ledger_created_at;
