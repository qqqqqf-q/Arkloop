-- +goose Up
UPDATE tool_provider_configs
SET group_name = 'read'
WHERE group_name = 'image_understanding';

UPDATE tool_provider_configs
SET provider_name = 'read.minimax'
WHERE provider_name = 'image_understanding.minimax';

UPDATE personas
SET conditional_tools_json = REPLACE(conditional_tools_json::text, '"understand_image"', '"read"')::jsonb
WHERE conditional_tools_json IS NOT NULL
  AND conditional_tools_json::text LIKE '%"understand_image"%';

-- +goose Down
UPDATE personas
SET conditional_tools_json = REPLACE(conditional_tools_json::text, '"read"', '"understand_image"')::jsonb
WHERE conditional_tools_json IS NOT NULL
  AND conditional_tools_json::text LIKE '%"read"%';

UPDATE tool_provider_configs
SET provider_name = 'image_understanding.minimax'
WHERE provider_name = 'read.minimax';

UPDATE tool_provider_configs
SET group_name = 'image_understanding'
WHERE group_name = 'read';
