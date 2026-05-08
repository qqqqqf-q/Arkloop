-- +goose Up
WITH group_config AS (
    SELECT DISTINCT ON (cil.channel_id)
           cil.channel_id,
           cil.heartbeat_enabled,
           cil.heartbeat_interval_minutes,
           cil.heartbeat_model
      FROM channel_identity_links AS cil
      JOIN channel_identities AS ci
        ON ci.id = cil.channel_identity_id
     WHERE ci.user_id IS NULL
     ORDER BY cil.channel_id, cil.updated_at DESC
),
owner_links AS (
    SELECT cil.id,
           gc.heartbeat_enabled,
           gc.heartbeat_interval_minutes,
           gc.heartbeat_model
      FROM group_config AS gc
      JOIN channels AS ch
        ON ch.id = gc.channel_id
      JOIN channel_identity_links AS cil
        ON cil.channel_id = gc.channel_id
      JOIN channel_identities AS ci
        ON ci.id = cil.channel_identity_id
       AND ci.user_id = ch.owner_user_id
     WHERE ch.owner_user_id IS NOT NULL
)
UPDATE channel_identity_links AS cil
   SET heartbeat_enabled = owner_links.heartbeat_enabled,
       heartbeat_interval_minutes = owner_links.heartbeat_interval_minutes,
       heartbeat_model = owner_links.heartbeat_model,
       updated_at = now()
  FROM owner_links
 WHERE cil.id = owner_links.id;

DELETE FROM channel_identity_links AS cil
 USING channel_identities AS ci
 WHERE ci.id = cil.channel_identity_id
   AND ci.user_id IS NULL;

DELETE FROM scheduled_triggers AS st
 USING channel_identities AS ci
 WHERE st.channel_identity_id = ci.id
   AND ci.user_id IS NOT NULL
   AND st.trigger_kind = 'heartbeat';

-- +goose Down
