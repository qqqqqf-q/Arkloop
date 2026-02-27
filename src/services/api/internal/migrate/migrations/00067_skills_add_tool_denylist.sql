-- +goose Up
ALTER TABLE skills ADD COLUMN tool_denylist TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE skills DROP COLUMN IF EXISTS tool_denylist;
