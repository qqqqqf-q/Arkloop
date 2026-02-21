-- +goose Up
ALTER TABLE users
    ADD COLUMN email             TEXT,
    ADD COLUMN email_verified_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN status            TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN deleted_at        TIMESTAMP WITH TIME ZONE,
    ADD COLUMN avatar_url        TEXT,
    ADD COLUMN locale            TEXT,
    ADD COLUMN timezone          TEXT,
    ADD COLUMN last_login_at     TIMESTAMP WITH TIME ZONE;

ALTER TABLE users
    ADD CONSTRAINT chk_users_status CHECK (status IN ('active', 'suspended', 'deleted'));

CREATE UNIQUE INDEX uq_users_email ON users (email) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS uq_users_email;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS chk_users_status,
    DROP COLUMN IF EXISTS last_login_at,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS locale,
    DROP COLUMN IF EXISTS avatar_url,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS email_verified_at,
    DROP COLUMN IF EXISTS email;
