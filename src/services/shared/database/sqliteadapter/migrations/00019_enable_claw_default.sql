-- +goose Up
UPDATE feature_flags SET default_value = 1 WHERE key = 'claw_enabled';

-- +goose Down
UPDATE feature_flags SET default_value = 0 WHERE key = 'claw_enabled';
