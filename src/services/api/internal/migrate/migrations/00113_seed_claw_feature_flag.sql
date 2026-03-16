-- +goose Up
INSERT INTO feature_flags (key, description, default_value)
VALUES ('claw_enabled', 'enable cloud claw mode', false)
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM feature_flags
WHERE key = 'claw_enabled';
