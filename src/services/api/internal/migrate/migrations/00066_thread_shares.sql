-- +goose Up
CREATE TABLE thread_shares (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id              UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    token                  VARCHAR(32) NOT NULL,
    access_type            VARCHAR(16) NOT NULL DEFAULT 'public',
    password_hash          TEXT,
    snapshot_message_count INT         NOT NULL,
    created_by_user_id     UUID        NOT NULL REFERENCES users(id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_thread_shares_token     ON thread_shares(token);
CREATE UNIQUE INDEX idx_thread_shares_thread_id ON thread_shares(thread_id);

-- +goose Down
DROP TABLE IF EXISTS thread_shares;
