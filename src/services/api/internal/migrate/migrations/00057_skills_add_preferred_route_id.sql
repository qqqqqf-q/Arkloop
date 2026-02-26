-- +goose Up
ALTER TABLE skills
    ADD COLUMN preferred_route_id TEXT;

-- +goose Down
ALTER TABLE skills
    DROP COLUMN IF EXISTS preferred_route_id;
