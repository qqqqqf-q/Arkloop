# Persona YAML Reference

Each persona directory contains `persona.yaml` (required) and `prompt.md` (optional).

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique persona key, matches directory name |
| `version` | string | no | Version string, defaults to `"1"` |
| `title` | string | yes | Display name shown in Console and API responses |
| `description` | string | no | Brief description of the persona's behavior |
| `tool_allowlist` | string[] | no | Allowed tool names (empty = all allowed) |
| `tool_denylist` | string[] | no | Denied tool names |
| `executor_type` | string | no | Execution engine, defaults to `agent.simple`. Use `agent.lua` for custom agent loop |
| `executor_config` | map | no | Engine-specific config. For `agent.lua`: `script_file: agent.lua` |
| `budgets` | map | no | `max_iterations`, `max_output_tokens`, `temperature` |
| `agent_config` | string | no | Model routing config name |
| `title_summarize` | map | no | `prompt` and `max_tokens` for conversation title generation |
| `user_selectable` | bool | no | Whether this persona appears in the Web frontend persona selector. Default `false` |
| `selector_name` | string | no | Label shown in the selector button and dropdown. Falls back to `title` if omitted |
| `selector_order` | int | no | Sort order in the selector dropdown. Lower values appear first. Unset defaults to `99` |

## Frontend Selector Behavior

The Web app fetches `/v1/personas` on mount, filters by `user_selectable: true`, and renders
a dropdown sorted by `selector_order`. Changing these fields requires an API service restart
to take effect (the YAML is read at startup).

If the API is unreachable, a hardcoded fallback is used (Normal + Search).
