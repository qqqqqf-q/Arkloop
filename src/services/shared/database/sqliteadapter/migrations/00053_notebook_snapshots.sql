-- Separate stable notebook snapshots from semantic memory snapshots.

-- +goose Up

CREATE TABLE user_notebook_snapshots (
    account_id     TEXT NOT NULL,
    user_id        TEXT NOT NULL,
    agent_id       TEXT NOT NULL DEFAULT 'default',
    notebook_block TEXT NOT NULL,
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, user_id, agent_id)
);

-- +goose Down

DROP TABLE IF EXISTS user_notebook_snapshots;
