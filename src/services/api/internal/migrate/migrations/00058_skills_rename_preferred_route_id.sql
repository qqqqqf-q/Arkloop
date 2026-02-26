-- +goose Up
ALTER TABLE skills
    RENAME COLUMN preferred_route_id TO preferred_credential;

-- +goose Down
ALTER TABLE skills
    RENAME COLUMN preferred_credential TO preferred_route_id;
