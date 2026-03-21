-- Align personas with PG migration 00137 (stream_thinking).

-- +goose Up

ALTER TABLE personas ADD COLUMN stream_thinking INTEGER NOT NULL DEFAULT 1;

-- +goose Down

-- SQLite: leave column in place.
