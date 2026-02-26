-- +goose Up
CREATE TABLE thread_stars (
    user_id    UUID        NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
    thread_id  UUID        NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, thread_id)
);

CREATE INDEX thread_stars_user_id_idx ON thread_stars (user_id);

-- +goose Down
DROP TABLE IF EXISTS thread_stars;
