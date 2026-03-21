-- +goose Up
ALTER TABLE personas
	ADD COLUMN IF NOT EXISTS stream_thinking BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE personas DROP COLUMN IF EXISTS stream_thinking;
