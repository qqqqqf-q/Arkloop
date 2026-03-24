-- +goose Up

-- SQLite compatibility:
-- New databases already create channels.owner_user_id in 00028.
-- Older databases are repaired in schema_compat.go after migrations.

-- +goose Down

-- SQLite: leave column in place.
