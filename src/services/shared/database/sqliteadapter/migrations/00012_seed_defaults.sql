-- Seed default data: feature flags

-- +goose Up

INSERT OR IGNORE INTO feature_flags (id, key, description, default_value)
VALUES ('a1b2c3d4-0000-4000-8000-000000000001', 'claw_enabled', 'Enable Claw mode for agents', 0);

-- +goose Down

DELETE FROM feature_flags WHERE key = 'claw_enabled';
