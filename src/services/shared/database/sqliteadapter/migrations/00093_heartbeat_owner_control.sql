-- +goose Up
UPDATE channel_identity_links
   SET heartbeat_enabled = COALESCE((
           SELECT group_link.heartbeat_enabled
             FROM channel_identity_links AS group_link
             JOIN channel_identities AS group_identity
               ON group_identity.id = group_link.channel_identity_id
            WHERE group_link.channel_id = channel_identity_links.channel_id
              AND group_identity.user_id IS NULL
            ORDER BY group_link.updated_at DESC
            LIMIT 1
       ), heartbeat_enabled),
       heartbeat_interval_minutes = COALESCE((
           SELECT group_link.heartbeat_interval_minutes
             FROM channel_identity_links AS group_link
             JOIN channel_identities AS group_identity
               ON group_identity.id = group_link.channel_identity_id
            WHERE group_link.channel_id = channel_identity_links.channel_id
              AND group_identity.user_id IS NULL
            ORDER BY group_link.updated_at DESC
            LIMIT 1
       ), heartbeat_interval_minutes),
       heartbeat_model = COALESCE((
           SELECT group_link.heartbeat_model
             FROM channel_identity_links AS group_link
             JOIN channel_identities AS group_identity
               ON group_identity.id = group_link.channel_identity_id
            WHERE group_link.channel_id = channel_identity_links.channel_id
              AND group_identity.user_id IS NULL
            ORDER BY group_link.updated_at DESC
            LIMIT 1
       ), heartbeat_model),
       updated_at = datetime('now')
 WHERE EXISTS (
       SELECT 1
         FROM channels AS ch
         JOIN channel_identities AS owner_identity
           ON owner_identity.id = channel_identity_links.channel_identity_id
          AND owner_identity.user_id = ch.owner_user_id
        WHERE ch.id = channel_identity_links.channel_id
          AND ch.owner_user_id IS NOT NULL
   )
   AND EXISTS (
       SELECT 1
         FROM channel_identity_links AS group_link
         JOIN channel_identities AS group_identity
           ON group_identity.id = group_link.channel_identity_id
        WHERE group_link.channel_id = channel_identity_links.channel_id
          AND group_identity.user_id IS NULL
   );

DELETE FROM channel_identity_links
 WHERE EXISTS (
       SELECT 1
         FROM channel_identities ci
        WHERE ci.id = channel_identity_links.channel_identity_id
          AND ci.user_id IS NULL
   );

DELETE FROM scheduled_triggers
 WHERE trigger_kind = 'heartbeat'
   AND EXISTS (
       SELECT 1
         FROM channel_identities ci
        WHERE ci.id = scheduled_triggers.channel_identity_id
          AND ci.user_id IS NOT NULL
   );

-- +goose Down
