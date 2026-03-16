-- Add show_in_picker column to llm_routes, matching PG migration 00128.

-- +goose Up

ALTER TABLE llm_routes ADD COLUMN show_in_picker INTEGER NOT NULL DEFAULT 1;

-- +goose Down

ALTER TABLE llm_routes DROP COLUMN show_in_picker;
