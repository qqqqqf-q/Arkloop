-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS uq_messages_user_client_message_id
    ON messages (account_id, thread_id, created_by_user_id, json_extract(metadata_json, '$.client_message_id'))
    WHERE role = 'user'
      AND deleted_at IS NULL
      AND COALESCE(json_extract(metadata_json, '$.client_message_id'), '') <> '';

-- +goose Down
DROP INDEX IF EXISTS uq_messages_user_client_message_id;
