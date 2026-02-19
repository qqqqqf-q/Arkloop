-- +goose Up
CREATE TABLE user_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    login TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT uq_user_credentials_user_id UNIQUE (user_id),
    CONSTRAINT uq_user_credentials_login UNIQUE (login)
);

-- +goose Down
DROP TABLE IF EXISTS user_credentials;
