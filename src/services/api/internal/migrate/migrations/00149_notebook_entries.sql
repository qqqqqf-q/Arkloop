CREATE TABLE notebook_entries (
    id         UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    account_id UUID NOT NULL,
    user_id    UUID NOT NULL,
    agent_id   TEXT NOT NULL DEFAULT 'default',
    scope      TEXT NOT NULL DEFAULT 'user',
    category   TEXT NOT NULL DEFAULT 'general',
    entry_key  TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notebook_entries_user
    ON notebook_entries (account_id, user_id, agent_id);

CREATE INDEX idx_notebook_entries_scope
    ON notebook_entries (account_id, user_id, agent_id, scope);

---- create above / drop below ----

DROP INDEX IF EXISTS idx_notebook_entries_scope;
DROP INDEX IF EXISTS idx_notebook_entries_user;
DROP TABLE IF EXISTS notebook_entries;
