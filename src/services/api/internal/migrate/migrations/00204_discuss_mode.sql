-- +goose Up
UPDATE scheduled_triggers
   SET trigger_kind = 'discuss',
       updated_at = now()
 WHERE trigger_kind = 'heartbeat';

UPDATE threads
   SET config_json =
       (
         CASE
           WHEN config_json ? 'heartbeat_enabled'
             THEN jsonb_set(COALESCE(config_json, '{}'::jsonb) - 'heartbeat_enabled', '{discuss_enabled}', config_json->'heartbeat_enabled', true)
           ELSE COALESCE(config_json, '{}'::jsonb)
         END
       ),
       updated_at = now()
 WHERE config_json ? 'heartbeat_enabled';

UPDATE threads
   SET config_json =
       (
         CASE
           WHEN config_json ? 'heartbeat_interval_minutes'
             THEN jsonb_set(COALESCE(config_json, '{}'::jsonb) - 'heartbeat_interval_minutes', '{discuss_interval_minutes}', config_json->'heartbeat_interval_minutes', true)
           ELSE COALESCE(config_json, '{}'::jsonb)
         END
       ),
       updated_at = now()
 WHERE config_json ? 'heartbeat_interval_minutes';

UPDATE threads
   SET config_json =
       (
         CASE
           WHEN config_json ? 'heartbeat_model'
             THEN jsonb_set(COALESCE(config_json, '{}'::jsonb) - 'heartbeat_model', '{discuss_model}', config_json->'heartbeat_model', true)
           ELSE COALESCE(config_json, '{}'::jsonb)
         END
       ),
       updated_at = now()
 WHERE config_json ? 'heartbeat_model';

-- +goose Down
UPDATE scheduled_triggers
   SET trigger_kind = 'heartbeat',
       updated_at = now()
 WHERE trigger_kind = 'discuss';

UPDATE threads
   SET config_json =
       (
         CASE
           WHEN config_json ? 'discuss_enabled'
             THEN jsonb_set(COALESCE(config_json, '{}'::jsonb) - 'discuss_enabled', '{heartbeat_enabled}', config_json->'discuss_enabled', true)
           ELSE COALESCE(config_json, '{}'::jsonb)
         END
       ),
       updated_at = now()
 WHERE config_json ? 'discuss_enabled';

UPDATE threads
   SET config_json =
       (
         CASE
           WHEN config_json ? 'discuss_interval_minutes'
             THEN jsonb_set(COALESCE(config_json, '{}'::jsonb) - 'discuss_interval_minutes', '{heartbeat_interval_minutes}', config_json->'discuss_interval_minutes', true)
           ELSE COALESCE(config_json, '{}'::jsonb)
         END
       ),
       updated_at = now()
 WHERE config_json ? 'discuss_interval_minutes';

UPDATE threads
   SET config_json =
       (
         CASE
           WHEN config_json ? 'discuss_model'
             THEN jsonb_set(COALESCE(config_json, '{}'::jsonb) - 'discuss_model', '{heartbeat_model}', config_json->'discuss_model', true)
           ELSE COALESCE(config_json, '{}'::jsonb)
         END
       ),
       updated_at = now()
 WHERE config_json ? 'discuss_model';
