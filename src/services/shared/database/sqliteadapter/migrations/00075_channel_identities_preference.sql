-- +goose Up
ALTER TABLE channel_identities ADD COLUMN preferred_model TEXT NOT NULL DEFAULT '';
ALTER TABLE channel_identities ADD COLUMN reasoning_mode TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE channel_identities DROP COLUMN preferred_model;
ALTER TABLE channel_identities DROP COLUMN reasoning_mode;
