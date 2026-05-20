-- +goose Up
ALTER TABLE personas ADD COLUMN image_model TEXT;

-- +goose Down
ALTER TABLE personas DROP COLUMN image_model;
