-- +goose Up

ALTER TABLE channels
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

-- +goose Down

ALTER TABLE channels DROP COLUMN IF EXISTS owner_user_id;
