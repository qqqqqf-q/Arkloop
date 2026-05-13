-- +goose Up
ALTER TABLE scheduled_triggers
    ADD COLUMN IF NOT EXISTS thread_id UUID REFERENCES threads(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_scheduled_triggers_thread_id
    ON scheduled_triggers (thread_id)
    WHERE thread_id IS NOT NULL;

DROP INDEX IF EXISTS scheduled_triggers_target_idx;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_thread_target_idx
    ON scheduled_triggers (thread_id)
    WHERE thread_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_identity_target_idx
    ON scheduled_triggers (channel_id, channel_identity_id)
    WHERE thread_id IS NULL;

DELETE FROM scheduled_triggers AS legacy
 USING scheduled_triggers AS thread_scoped
 WHERE legacy.thread_id IS NULL
   AND thread_scoped.thread_id IS NOT NULL
   AND legacy.channel_id = thread_scoped.channel_id
   AND legacy.channel_identity_id = thread_scoped.channel_identity_id;

-- +goose Down
DROP INDEX IF EXISTS scheduled_triggers_identity_target_idx;
DROP INDEX IF EXISTS scheduled_triggers_thread_target_idx;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_triggers_target_idx
    ON scheduled_triggers (channel_id, channel_identity_id);

DROP INDEX IF EXISTS idx_scheduled_triggers_thread_id;

ALTER TABLE scheduled_triggers
    DROP COLUMN IF EXISTS thread_id;
