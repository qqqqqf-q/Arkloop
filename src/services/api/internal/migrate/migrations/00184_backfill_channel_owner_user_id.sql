-- +goose Up

UPDATE channels AS ch
   SET owner_user_id = a.owner_user_id
  FROM accounts AS a
 WHERE ch.account_id = a.id
   AND ch.owner_user_id IS NULL
   AND a.owner_user_id IS NOT NULL;

-- +goose Down

-- Data backfill is intentionally not reversed.
