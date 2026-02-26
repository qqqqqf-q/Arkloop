-- +goose Up
ALTER TABLE users RENAME COLUMN display_name TO username;

-- +goose Down
ALTER TABLE users RENAME COLUMN username TO display_name;
