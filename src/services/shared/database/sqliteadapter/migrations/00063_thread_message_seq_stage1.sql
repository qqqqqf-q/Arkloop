-- +goose Up
ALTER TABLE threads ADD COLUMN next_message_seq INTEGER NOT NULL DEFAULT 1;
ALTER TABLE messages ADD COLUMN thread_seq INTEGER;

-- +goose Down
SELECT 1;
