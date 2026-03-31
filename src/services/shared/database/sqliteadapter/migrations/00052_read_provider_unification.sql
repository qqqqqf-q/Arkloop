-- Align Desktop SQLite with PG migration 00146 (read provider unification).

-- +goose Up

UPDATE tool_provider_configs
SET group_name = 'read'
WHERE group_name = 'image_understanding';

UPDATE tool_provider_configs
SET provider_name = 'read.minimax'
WHERE provider_name = 'image_understanding.minimax';

UPDATE personas
SET conditional_tools_json = REPLACE(conditional_tools_json, '"understand_image"', '"read"')
WHERE conditional_tools_json IS NOT NULL
  AND conditional_tools_json LIKE '%"understand_image"%';

-- +goose Down

UPDATE personas
SET conditional_tools_json = REPLACE(conditional_tools_json, '"read"', '"understand_image"')
WHERE conditional_tools_json IS NOT NULL
  AND conditional_tools_json LIKE '%"read"%';

UPDATE tool_provider_configs
SET provider_name = 'image_understanding.minimax'
WHERE provider_name = 'read.minimax';

UPDATE tool_provider_configs
SET group_name = 'image_understanding'
WHERE group_name = 'read';
