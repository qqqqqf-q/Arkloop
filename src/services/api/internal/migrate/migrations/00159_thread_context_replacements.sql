-- +goose Up
CREATE TABLE thread_context_replacements (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id       UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id        UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    start_thread_seq BIGINT NOT NULL,
    end_thread_seq   BIGINT NOT NULL,
    summary_text     TEXT NOT NULL,
    layer            INTEGER NOT NULL DEFAULT 1,
    metadata_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    superseded_at    TIMESTAMPTZ NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_thread_context_replacements_range CHECK (start_thread_seq <= end_thread_seq)
);

CREATE INDEX idx_thread_context_replacements_thread_active
    ON thread_context_replacements(thread_id, start_thread_seq, end_thread_seq, layer DESC, created_at DESC)
    WHERE superseded_at IS NULL;

CREATE INDEX idx_thread_context_replacements_thread_created
    ON thread_context_replacements(thread_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_thread_context_replacements_thread_created;
DROP INDEX IF EXISTS idx_thread_context_replacements_thread_active;
DROP TABLE IF EXISTS thread_context_replacements;
