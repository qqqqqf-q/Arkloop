-- +goose Up
ALTER TABLE threads
    ADD COLUMN IF NOT EXISTS next_message_seq BIGINT NOT NULL DEFAULT 1;

ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS thread_seq BIGINT;

CREATE OR REPLACE FUNCTION assign_message_thread_seq() RETURNS trigger AS $$
BEGIN
    IF NEW.thread_seq IS NULL THEN
        UPDATE threads
           SET next_message_seq = next_message_seq + 1
         WHERE id = NEW.thread_id
           AND account_id = NEW.account_id
        RETURNING next_message_seq - 1 INTO NEW.thread_seq;
        IF NEW.thread_seq IS NULL THEN
            RAISE EXCEPTION 'thread % for account % does not exist', NEW.thread_id, NEW.account_id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_messages_assign_thread_seq ON messages;
CREATE TRIGGER trg_messages_assign_thread_seq
    BEFORE INSERT ON messages
    FOR EACH ROW
    EXECUTE FUNCTION assign_message_thread_seq();

-- +goose Down
DROP TRIGGER IF EXISTS trg_messages_assign_thread_seq ON messages;
DROP FUNCTION IF EXISTS assign_message_thread_seq();

ALTER TABLE messages
    DROP COLUMN IF EXISTS thread_seq;

ALTER TABLE threads
    DROP COLUMN IF EXISTS next_message_seq;
