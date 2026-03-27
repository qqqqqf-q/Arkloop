-- Align personas with PG migration 00142 (conditional_tools_json).

-- +goose Up

ALTER TABLE personas ADD COLUMN conditional_tools_json TEXT;

-- +goose Down

-- SQLite: leave column in place.
