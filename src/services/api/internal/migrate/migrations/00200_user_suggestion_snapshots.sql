-- +goose Up
CREATE TABLE IF NOT EXISTS user_suggestion_snapshots (
    account_id        UUID NOT NULL,
    user_id           UUID NOT NULL,
    agent_id          TEXT NOT NULL DEFAULT 'default',
    mode              TEXT NOT NULL DEFAULT 'chat',
    suggestions_json  TEXT NOT NULL DEFAULT '[]',
    suggestion_score  INTEGER NOT NULL DEFAULT 0,
    last_build_at     TIMESTAMPTZ,
    expires_at        TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, user_id, agent_id, mode)
);

-- +goose Down
DROP TABLE IF EXISTS user_suggestion_snapshots;
