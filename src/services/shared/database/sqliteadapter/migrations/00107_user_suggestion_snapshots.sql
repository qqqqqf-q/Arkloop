-- +goose Up
CREATE TABLE IF NOT EXISTS user_suggestion_snapshots (
    account_id        TEXT NOT NULL,
    user_id           TEXT NOT NULL,
    agent_id          TEXT NOT NULL DEFAULT 'default',
    mode              TEXT NOT NULL DEFAULT 'chat',
    suggestions_json  TEXT NOT NULL DEFAULT '[]',
    suggestion_score  INTEGER NOT NULL DEFAULT 0,
    last_build_at     TEXT,
    expires_at        TEXT,
    updated_at        TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, user_id, agent_id, mode)
);

-- +goose Down
DROP TABLE IF EXISTS user_suggestion_snapshots;
