-- +goose Up
CREATE TABLE scheduled_triggers_thread_v2 (
    id                    TEXT PRIMARY KEY,
    channel_id            TEXT NOT NULL,
    channel_identity_id   TEXT NOT NULL,
    thread_id             TEXT REFERENCES threads(id) ON DELETE CASCADE,
    persona_key           TEXT NOT NULL,
    account_id            TEXT NOT NULL,
    model                 TEXT NOT NULL DEFAULT '',
    interval_min          INTEGER NOT NULL DEFAULT 30,
    next_fire_at          TEXT NOT NULL,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    trigger_kind          TEXT NOT NULL DEFAULT 'heartbeat',
    job_id                TEXT,
    cooldown_level        INTEGER NOT NULL DEFAULT 0,
    last_user_msg_at      TEXT,
    burst_start_at        TEXT
);

INSERT INTO scheduled_triggers_thread_v2 (
    id,
    channel_id,
    channel_identity_id,
    thread_id,
    persona_key,
    account_id,
    model,
    interval_min,
    next_fire_at,
    created_at,
    updated_at,
    trigger_kind,
    job_id,
    cooldown_level,
    last_user_msg_at,
    burst_start_at
)
SELECT
    id,
    channel_id,
    channel_identity_id,
    NULL,
    persona_key,
    account_id,
    model,
    interval_min,
    next_fire_at,
    created_at,
    updated_at,
    trigger_kind,
    job_id,
    cooldown_level,
    last_user_msg_at,
    burst_start_at
  FROM scheduled_triggers;

DROP TABLE scheduled_triggers;

ALTER TABLE scheduled_triggers_thread_v2 RENAME TO scheduled_triggers;

CREATE INDEX IF NOT EXISTS scheduled_triggers_next_fire_at_idx
    ON scheduled_triggers (next_fire_at);

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_job_id_uniq
    ON scheduled_triggers (job_id)
    WHERE job_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_scheduled_triggers_thread_id
    ON scheduled_triggers (thread_id)
    WHERE thread_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_thread_target_idx
    ON scheduled_triggers (thread_id)
    WHERE thread_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_identity_target_idx
    ON scheduled_triggers (channel_id, channel_identity_id)
    WHERE thread_id IS NULL;

DELETE FROM scheduled_triggers
 WHERE thread_id IS NULL
   AND EXISTS (
       SELECT 1
         FROM scheduled_triggers AS thread_scoped
        WHERE thread_scoped.thread_id IS NOT NULL
          AND thread_scoped.channel_id = scheduled_triggers.channel_id
          AND thread_scoped.channel_identity_id = scheduled_triggers.channel_identity_id
   );

-- +goose Down
DROP INDEX IF EXISTS scheduled_triggers_identity_target_idx;
DROP INDEX IF EXISTS scheduled_triggers_thread_target_idx;
DROP INDEX IF EXISTS scheduled_triggers_job_id_uniq;
DROP INDEX IF EXISTS scheduled_triggers_next_fire_at_idx;

CREATE TABLE scheduled_triggers_identity_v1 (
    id                    TEXT PRIMARY KEY,
    channel_id            TEXT NOT NULL,
    channel_identity_id   TEXT NOT NULL,
    persona_key           TEXT NOT NULL,
    account_id            TEXT NOT NULL,
    model                 TEXT NOT NULL DEFAULT '',
    interval_min          INTEGER NOT NULL DEFAULT 30,
    next_fire_at          TEXT NOT NULL,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    trigger_kind          TEXT NOT NULL DEFAULT 'heartbeat',
    job_id                TEXT,
    cooldown_level        INTEGER NOT NULL DEFAULT 0,
    last_user_msg_at      TEXT,
    burst_start_at        TEXT,
    UNIQUE (channel_id, channel_identity_id)
);

INSERT OR IGNORE INTO scheduled_triggers_identity_v1 (
    id,
    channel_id,
    channel_identity_id,
    persona_key,
    account_id,
    model,
    interval_min,
    next_fire_at,
    created_at,
    updated_at,
    trigger_kind,
    job_id,
    cooldown_level,
    last_user_msg_at,
    burst_start_at
)
SELECT
    id,
    channel_id,
    channel_identity_id,
    persona_key,
    account_id,
    model,
    interval_min,
    next_fire_at,
    created_at,
    updated_at,
    trigger_kind,
    job_id,
    cooldown_level,
    last_user_msg_at,
    burst_start_at
  FROM scheduled_triggers
 ORDER BY thread_id IS NOT NULL, updated_at DESC;

CREATE INDEX IF NOT EXISTS scheduled_triggers_target_idx
    ON scheduled_triggers_identity_v1 (channel_id, channel_identity_id);

DROP INDEX IF EXISTS idx_scheduled_triggers_thread_id;

DROP TABLE scheduled_triggers;

ALTER TABLE scheduled_triggers_identity_v1 RENAME TO scheduled_triggers;

CREATE INDEX IF NOT EXISTS scheduled_triggers_next_fire_at_idx
    ON scheduled_triggers (next_fire_at);

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_job_id_uniq
    ON scheduled_triggers (job_id)
    WHERE job_id IS NOT NULL;
