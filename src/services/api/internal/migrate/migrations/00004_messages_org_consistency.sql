-- +goose Up
ALTER TABLE messages ADD COLUMN org_id UUID;
ALTER TABLE messages ADD COLUMN created_by_user_id UUID;

-- backfill org_id from threads (no-op on fresh database)
UPDATE messages AS m
SET org_id = t.org_id
FROM threads AS t
WHERE m.thread_id = t.id
  AND m.org_id IS NULL;

ALTER TABLE messages ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE threads ADD CONSTRAINT uq_threads_id_org_id UNIQUE (id, org_id);
ALTER TABLE messages DROP CONSTRAINT messages_thread_id_fkey;

ALTER TABLE messages ADD CONSTRAINT fk_messages_org_id_orgs
    FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE;

ALTER TABLE messages ADD CONSTRAINT fk_messages_created_by_user_id_users
    FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE messages ADD CONSTRAINT fk_messages_thread_org
    FOREIGN KEY (thread_id, org_id) REFERENCES threads(id, org_id) ON DELETE CASCADE;

CREATE INDEX ix_messages_org_id_thread_id_created_at
    ON messages(org_id, thread_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS ix_messages_org_id_thread_id_created_at;

ALTER TABLE messages DROP CONSTRAINT IF EXISTS fk_messages_thread_org;
ALTER TABLE messages DROP CONSTRAINT IF EXISTS fk_messages_created_by_user_id_users;
ALTER TABLE messages DROP CONSTRAINT IF EXISTS fk_messages_org_id_orgs;

ALTER TABLE messages ADD CONSTRAINT messages_thread_id_fkey
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE;

ALTER TABLE threads DROP CONSTRAINT IF EXISTS uq_threads_id_org_id;

ALTER TABLE messages DROP COLUMN IF EXISTS created_by_user_id;
ALTER TABLE messages DROP COLUMN IF EXISTS org_id;
