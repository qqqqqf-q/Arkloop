-- +goose Up
CREATE TABLE thread_compaction_snapshots (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id             UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id              UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    summary_text           TEXT NOT NULL,
    metadata_json          JSONB NOT NULL DEFAULT '{}'::jsonb,
    supersedes_snapshot_id UUID REFERENCES thread_compaction_snapshots(id) ON DELETE SET NULL,
    is_active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_thread_compaction_snapshots_active_thread
    ON thread_compaction_snapshots(thread_id)
    WHERE is_active = TRUE;

CREATE INDEX ix_thread_compaction_snapshots_thread_created_at
    ON thread_compaction_snapshots(thread_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS ix_thread_compaction_snapshots_thread_created_at;
DROP INDEX IF EXISTS uq_thread_compaction_snapshots_active_thread;
DROP TABLE IF EXISTS thread_compaction_snapshots;
