---
---

# Persona YAML Reference

Persona definitions live in `src/personas/<id>/`, each directory containing:

- `persona.yaml` (required) -- persona configuration
- `prompt.md` (optional) -- system prompt
- `agent.lua` (optional) -- custom agent loop script (executor_type: agent.lua)

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique identifier, matches directory name |
| `version` | string | no | Version string, defaults to `"1"` |
| `title` | string | yes | Display name used in Console and API responses |
| `description` | string | no | Brief description |
| `tool_allowlist` | string[] | no | Allowed tool names. Empty = all allowed |
| `tool_denylist` | string[] | no | Denied tool names |
| `executor_type` | string | no | Execution engine, defaults to `agent.simple`. Use `agent.lua` for custom scripts |
| `executor_config` | map | no | Engine config. For `agent.lua`: `script_file: agent.lua` |
| `budgets` | map | no | `max_iterations`, `max_output_tokens`, `temperature` |
| `agent_config` | string | no | Model routing config name |
| `title_summarize` | map | no | Conversation title generation: `prompt` + `max_tokens` |
| `user_selectable` | bool | no | Whether this persona appears in the Web frontend selector. Default `false` |
| `selector_name` | string | no | Label shown in the selector. Falls back to `title` if omitted |
| `selector_order` | int | no | Sort weight in the selector dropdown. Lower values appear first. Default `99` |

## Frontend Selector

The Web frontend fetches `GET /v1/personas` on mount, filters entries with `user_selectable: true`,
and renders a dropdown sorted by `selector_order` ascending.

After modifying YAML files, you must **restart the API service** for changes to take effect
(personas are loaded from the filesystem once at API startup).

If the API is unreachable, the selector button displays the raw persona key and the dropdown is disabled.

## Example

```yaml
id: normal
version: "1"
title: Normal
description: General conversation mode
budgets:
  max_iterations: 99999
  max_output_tokens: 20480
  temperature: 1.0
agent_config: openrouter-oss-120b-groq

# frontend selector
user_selectable: true
selector_name: Normal
selector_order: 1
```
