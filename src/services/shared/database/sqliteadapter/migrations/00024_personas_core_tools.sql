-- Align personas table with PG migration 00129 (core_tools column).
-- SQLite stores TEXT[] as a JSON array string (e.g. '[]', '["tool_a"]').

-- +goose Up

ALTER TABLE personas ADD COLUMN core_tools TEXT NOT NULL DEFAULT '[]';

-- +goose Down

-- SQLite does not support DROP COLUMN in older versions; leave it in place.
