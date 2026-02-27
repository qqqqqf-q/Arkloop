-- +goose Up
ALTER TABLE threads
    ADD COLUMN parent_thread_id UUID REFERENCES threads(id) ON DELETE SET NULL,
    ADD COLUMN branched_from_message_id UUID REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX idx_threads_parent_thread_id ON threads(parent_thread_id) WHERE parent_thread_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_threads_parent_thread_id;
ALTER TABLE threads
    DROP COLUMN IF EXISTS branched_from_message_id,
    DROP COLUMN IF EXISTS parent_thread_id;
