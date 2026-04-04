-- +goose Up
UPDATE threads SET mode = 'work' WHERE mode = 'claw';
ALTER TABLE threads DROP CONSTRAINT chk_threads_mode;
ALTER TABLE threads ADD CONSTRAINT chk_threads_mode CHECK (mode IN ('chat', 'work'));
UPDATE feature_flags SET key = 'work_enabled', description = 'enable work mode' WHERE key = 'claw_enabled';

-- +goose Down
UPDATE feature_flags SET key = 'claw_enabled', description = 'enable cloud claw mode' WHERE key = 'work_enabled';
ALTER TABLE threads DROP CONSTRAINT chk_threads_mode;
ALTER TABLE threads ADD CONSTRAINT chk_threads_mode CHECK (mode IN ('chat', 'claw'));
UPDATE threads SET mode = 'claw' WHERE mode = 'work';
