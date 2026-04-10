-- +goose Up
WITH visible_spine AS (
    SELECT m.id,
           m.thread_id,
           ROW_NUMBER() OVER (PARTITION BY m.thread_id ORDER BY m.created_at ASC, m.id ASC) AS spine_pos
      FROM messages m
     WHERE m.deleted_at IS NULL
       AND m.hidden = FALSE
       AND COALESCE(m.compacted, FALSE) = FALSE
),
all_messages AS (
    SELECT m.id,
           m.thread_id,
           m.created_at,
           NULLIF(m.metadata_json->>'run_id', '') AS run_id,
           COALESCE(m.metadata_json->>'intermediate', '') = 'true' AS is_intermediate,
           ROW_NUMBER() OVER (PARTITION BY m.thread_id ORDER BY m.created_at ASC, m.id ASC) AS current_pos
      FROM messages m
),
run_started AS (
    SELECT started.run_id,
           started.anchor_message_id
      FROM (
            SELECT re.run_id,
                   NULLIF(re.data_json->>'thread_tail_message_id', '')::uuid AS anchor_message_id,
                   ROW_NUMBER() OVER (PARTITION BY re.run_id ORDER BY re.seq ASC, re.event_id ASC) AS row_num
              FROM run_events re
             WHERE re.type = 'run.started'
           ) started
     WHERE started.row_num = 1
),
run_assistant AS (
    SELECT m.thread_id,
           NULLIF(m.metadata_json->>'run_id', '') AS run_id,
           MIN(vs.spine_pos) AS assistant_spine_pos
      FROM messages m
      JOIN visible_spine vs ON vs.id = m.id
     WHERE m.deleted_at IS NULL
       AND m.hidden = FALSE
       AND m.role = 'assistant'
       AND NULLIF(m.metadata_json->>'run_id', '') IS NOT NULL
     GROUP BY m.thread_id, NULLIF(m.metadata_json->>'run_id', '')
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
        ON rs.run_id::text = im.run_id
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
           am.created_at,
           CASE
               WHEN NOT am.is_intermediate THEN am.current_pos::bigint * 1000000 + 500000
               WHEN rb.assistant_spine_pos IS NOT NULL
                   THEN rb.assistant_spine_pos::bigint * 1000000 - 800000 + rb.before_rank::bigint * 10000 + COALESCE(im.local_pos, 0)
               WHEN rb.anchor_spine_pos IS NOT NULL
                   THEN rb.anchor_spine_pos::bigint * 1000000 + 100 + rb.after_rank::bigint * 10000 + COALESCE(im.local_pos, 0)
               ELSE am.current_pos::bigint * 1000000 + 900000 + COALESCE(im.local_pos, 0)
           END AS sort_key
      FROM all_messages am
      LEFT JOIN intermediate_messages im
        ON im.id = am.id
      LEFT JOIN ranked_blocks rb
        ON rb.thread_id = am.thread_id
       AND rb.run_id = am.run_id
),
reseq AS (
    SELECT o.id,
           o.thread_id,
           ROW_NUMBER() OVER (
               PARTITION BY o.thread_id
               ORDER BY o.sort_key ASC, o.created_at ASC, o.id ASC
           ) AS new_seq
      FROM ordered o
)
UPDATE messages m
   SET thread_seq = r.new_seq
  FROM reseq r
 WHERE m.id = r.id;

UPDATE threads t
   SET next_message_seq = COALESCE(seq.max_seq, 0) + 1
  FROM (
        SELECT thread_id, MAX(thread_seq) AS max_seq
          FROM messages
         GROUP BY thread_id
       ) seq
 WHERE t.id = seq.thread_id;

UPDATE threads
   SET next_message_seq = 1
 WHERE next_message_seq IS NULL
    OR next_message_seq < 1;

ALTER TABLE messages
    ALTER COLUMN thread_seq SET NOT NULL;

DROP INDEX IF EXISTS ix_messages_account_id_thread_id_thread_seq;
CREATE INDEX ix_messages_account_id_thread_id_thread_seq
    ON messages (account_id, thread_id, thread_seq);

DROP INDEX IF EXISTS ix_messages_thread_id_thread_seq;
CREATE INDEX ix_messages_thread_id_thread_seq
    ON messages (thread_id, thread_seq);

DROP INDEX IF EXISTS uq_messages_thread_id_thread_seq;
CREATE UNIQUE INDEX uq_messages_thread_id_thread_seq
    ON messages (thread_id, thread_seq);

-- +goose Down
DROP INDEX IF EXISTS uq_messages_thread_id_thread_seq;
DROP INDEX IF EXISTS ix_messages_thread_id_thread_seq;
DROP INDEX IF EXISTS ix_messages_account_id_thread_id_thread_seq;

ALTER TABLE messages
    ALTER COLUMN thread_seq DROP NOT NULL;
