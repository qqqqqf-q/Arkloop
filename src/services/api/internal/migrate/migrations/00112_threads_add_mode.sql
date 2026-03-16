-- +goose Up
ALTER TABLE threads ADD COLUMN mode TEXT;

UPDATE threads
SET mode = 'chat'
WHERE mode IS NULL;

ALTER TABLE threads
    ALTER COLUMN mode SET DEFAULT 'chat',
    ALTER COLUMN mode SET NOT NULL,
    ADD CONSTRAINT chk_threads_mode CHECK (mode IN ('chat', 'claw'));

-- +goose Down
ALTER TABLE threads
    DROP CONSTRAINT IF EXISTS chk_threads_mode;

ALTER TABLE threads
    DROP COLUMN IF EXISTS mode;
