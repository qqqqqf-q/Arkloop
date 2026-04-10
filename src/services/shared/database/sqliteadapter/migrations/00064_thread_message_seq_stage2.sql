-- +goose NO TRANSACTION
-- +goose Up

PRAGMA foreign_keys = OFF;

DROP TABLE IF EXISTS _message_reseq_00064;
CREATE TEMP TABLE _message_reseq_00064 AS
WITH visible_spine AS (
    SELECT m.id,
           m.thread_id,
           ROW_NUMBER() OVER (PARTITION BY m.thread_id ORDER BY m.created_at ASC, m.id ASC) AS spine_pos
      FROM messages m
     WHERE m.deleted_at IS NULL
       AND m.hidden = 0
       AND COALESCE(m.compacted, 0) = 0
),
all_messages AS (
    SELECT m.id,
           m.thread_id,
           m.created_at,
           NULLIF(json_extract(m.metadata_json, '$.run_id'), '') AS run_id,
           COALESCE(json_extract(m.metadata_json, '$.intermediate'), '') = 'true' AS is_intermediate,
           ROW_NUMBER() OVER (PARTITION BY m.thread_id ORDER BY m.created_at ASC, m.id ASC) AS current_pos
      FROM messages m
),
run_started AS (
    SELECT started.run_id,
           started.anchor_message_id
      FROM (
            SELECT re.run_id,
                   NULLIF(json_extract(re.data_json, '$.thread_tail_message_id'), '') AS anchor_message_id,
                   ROW_NUMBER() OVER (PARTITION BY re.run_id ORDER BY re.seq ASC, re.event_id ASC) AS row_num
              FROM run_events re
             WHERE re.type = 'run.started'
           ) started
     WHERE started.row_num = 1
),
run_assistant AS (
    SELECT m.thread_id,
           NULLIF(json_extract(m.metadata_json, '$.run_id'), '') AS run_id,
           MIN(vs.spine_pos) AS assistant_spine_pos
      FROM messages m
      JOIN visible_spine vs ON vs.id = m.id
     WHERE m.deleted_at IS NULL
       AND m.hidden = 0
       AND m.role = 'assistant'
       AND NULLIF(json_extract(m.metadata_json, '$.run_id'), '') IS NOT NULL
     GROUP BY m.thread_id, NULLIF(json_extract(m.metadata_json, '$.run_id'), '')
),
intermediate_messages AS (
    SELECT am.id,
           am.thread_id,
           am.run_id,
           am.current_pos,
           am.created_at,
           ROW_NUMBER() OVER (PARTITION BY am.thread_id, am.run_id ORDER BY am.created_at ASC, am.id ASC) AS local_pos
      FROM all_messages am
     WHERE am.is_intermediate
       AND am.run_id IS NOT NULL
),
run_blocks AS (
    SELECT im.thread_id,
           im.run_id,
           MIN(im.current_pos) AS min_current_pos,
           ra.assistant_spine_pos,
           anchor_vs.spine_pos AS anchor_spine_pos
      FROM intermediate_messages im
      LEFT JOIN run_assistant ra
        ON ra.thread_id = im.thread_id
       AND ra.run_id = im.run_id
      LEFT JOIN run_started rs
        ON rs.run_id = im.run_id
      LEFT JOIN visible_spine anchor_vs
        ON anchor_vs.id = rs.anchor_message_id
     GROUP BY im.thread_id, im.run_id, ra.assistant_spine_pos, anchor_vs.spine_pos
),
ranked_blocks AS (
    SELECT rb.*,
           CASE
               WHEN rb.assistant_spine_pos IS NULL THEN 0
               ELSE ROW_NUMBER() OVER (
                   PARTITION BY rb.thread_id, rb.assistant_spine_pos
                   ORDER BY rb.min_current_pos ASC, rb.run_id ASC
               )
           END AS before_rank,
           CASE
               WHEN rb.assistant_spine_pos IS NOT NULL OR rb.anchor_spine_pos IS NULL THEN 0
               ELSE ROW_NUMBER() OVER (
                   PARTITION BY rb.thread_id, rb.anchor_spine_pos
                   ORDER BY rb.min_current_pos ASC, rb.run_id ASC
               )
           END AS after_rank
      FROM run_blocks rb
),
ordered AS (
    SELECT am.id,
           am.thread_id,
           CASE
               WHEN am.is_intermediate = 0 THEN am.current_pos * 1000000 + 500000
               WHEN rb.assistant_spine_pos IS NOT NULL
                   THEN rb.assistant_spine_pos * 1000000 - 800000 + rb.before_rank * 10000 + COALESCE(im.local_pos, 0)
               WHEN rb.anchor_spine_pos IS NOT NULL
                   THEN rb.anchor_spine_pos * 1000000 + 100 + rb.after_rank * 10000 + COALESCE(im.local_pos, 0)
               ELSE am.current_pos * 1000000 + 900000 + COALESCE(im.local_pos, 0)
           END AS sort_key,
           am.created_at
      FROM all_messages am
      LEFT JOIN intermediate_messages im
        ON im.id = am.id
      LEFT JOIN ranked_blocks rb
        ON rb.thread_id = am.thread_id
       AND rb.run_id = am.run_id
)
SELECT o.id,
       o.thread_id,
       ROW_NUMBER() OVER (
           PARTITION BY o.thread_id
           ORDER BY o.sort_key ASC, o.created_at ASC, o.id ASC
       ) AS new_seq
  FROM ordered o;

UPDATE messages
   SET thread_seq = (
       SELECT new_seq
         FROM _message_reseq_00064 r
        WHERE r.id = messages.id
   );

UPDATE threads
   SET next_message_seq = COALESCE((
       SELECT MAX(r.new_seq) + 1
         FROM _message_reseq_00064 r
        WHERE r.thread_id = threads.id
   ), 1);

ALTER TABLE messages RENAME TO messages_old_00064;
CREATE TABLE messages (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id          TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    account_id         TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_seq         INTEGER NOT NULL,
    created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    role               TEXT NOT NULL,
    content            TEXT NOT NULL,
    content_json       TEXT,
    metadata_json      TEXT NOT NULL DEFAULT '{}',
    hidden             INTEGER NOT NULL DEFAULT 0,
    deleted_at         TEXT,
    token_count        INTEGER,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    compacted          INTEGER NOT NULL DEFAULT 0
);
INSERT INTO messages (
    id, thread_id, account_id, thread_seq, created_by_user_id, role, content, content_json,
    metadata_json, hidden, deleted_at, token_count, created_at, compacted
)
SELECT
    id, thread_id, account_id, thread_seq, created_by_user_id, role, content, content_json,
    metadata_json, hidden, deleted_at, token_count, created_at, compacted
FROM messages_old_00064;
DROP TABLE messages_old_00064;

ALTER TABLE channel_message_ledger RENAME TO channel_message_ledger_old_00064;
CREATE TABLE channel_message_ledger (
    id                         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id                 TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_type               TEXT NOT NULL,
    direction                  TEXT NOT NULL,
    thread_id                  TEXT REFERENCES threads(id) ON DELETE SET NULL,
    run_id                     TEXT REFERENCES runs(id) ON DELETE SET NULL,
    platform_conversation_id   TEXT NOT NULL,
    platform_message_id        TEXT NOT NULL,
    platform_parent_message_id TEXT,
    platform_thread_id         TEXT,
    sender_channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE SET NULL,
    metadata_json              TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    message_id                 TEXT REFERENCES messages(id) ON DELETE SET NULL,
    CHECK (direction IN ('inbound', 'outbound')),
    UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
);
INSERT INTO channel_message_ledger (
    id, channel_id, channel_type, direction, thread_id, run_id, platform_conversation_id,
    platform_message_id, platform_parent_message_id, platform_thread_id,
    sender_channel_identity_id, metadata_json, created_at, message_id
)
SELECT
    id, channel_id, channel_type, direction, thread_id, run_id, platform_conversation_id,
    platform_message_id, platform_parent_message_id, platform_thread_id,
    sender_channel_identity_id, metadata_json, created_at, message_id
FROM channel_message_ledger_old_00064;
DROP TABLE channel_message_ledger_old_00064;

CREATE INDEX ix_messages_thread_id ON messages(thread_id);
CREATE INDEX ix_messages_org_id_thread_id_created_at ON messages(account_id, thread_id, created_at);
CREATE INDEX ix_messages_account_id_thread_id_thread_seq ON messages(account_id, thread_id, thread_seq);
CREATE INDEX ix_messages_thread_id_thread_seq ON messages(thread_id, thread_seq);
CREATE UNIQUE INDEX uq_messages_thread_id_thread_seq ON messages(thread_id, thread_seq);
CREATE INDEX ix_messages_thread_compacted
    ON messages (thread_id, compacted)
    WHERE deleted_at IS NULL AND compacted = 1;
CREATE INDEX idx_channel_message_ledger_channel_id ON channel_message_ledger(channel_id);
CREATE INDEX idx_channel_message_ledger_thread_id ON channel_message_ledger(thread_id);
CREATE INDEX idx_channel_message_ledger_run_id ON channel_message_ledger(run_id);
CREATE INDEX idx_channel_message_ledger_sender_identity_id ON channel_message_ledger(sender_channel_identity_id);
CREATE INDEX idx_channel_message_ledger_message_id ON channel_message_ledger(message_id);

DROP TABLE IF EXISTS _message_reseq_00064;

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT 1;
