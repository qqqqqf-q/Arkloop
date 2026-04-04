-- +goose Up
CREATE TABLE user_notebook_snapshots (
    account_id UUID NOT NULL,
    user_id UUID NOT NULL,
    agent_id TEXT NOT NULL,
    notebook_block TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, user_id, agent_id)
);

-- +goose Down
DROP TABLE IF EXISTS user_notebook_snapshots;
