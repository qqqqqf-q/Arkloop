-- Persona definitions: personas

-- +goose Up

CREATE TABLE personas (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id              TEXT,
    persona_key         TEXT NOT NULL,
    version             TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    description         TEXT,
    prompt_md           TEXT NOT NULL,
    tool_allowlist      TEXT NOT NULL DEFAULT '[]',
    tool_denylist       TEXT NOT NULL DEFAULT '[]',
    budgets_json        TEXT NOT NULL DEFAULT '{}',
    is_active           INTEGER NOT NULL DEFAULT 1,
    executor_type       TEXT NOT NULL DEFAULT 'agent.simple',
    executor_config_json TEXT NOT NULL DEFAULT '{}',
    preferred_credential TEXT,
    model               TEXT,
    reasoning_mode      TEXT NOT NULL DEFAULT 'auto',
    prompt_cache_control TEXT NOT NULL DEFAULT 'none',
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (org_id, persona_key, version)
);

-- +goose Down

DROP TABLE IF EXISTS personas;
