# Agent Configs & Prompt Templates

All endpoints require Bearer Token (or API Key) authentication.

---

## Prompt Templates

### Create Template

```http
POST /v1/prompt-templates
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | |
| `content` | `string` | No | Template content (supports `{{variable}}` placeholders) |
| `variables` | `[]string` | No | List of declared variable names |
| `is_default` | `bool` | No | Whether this is the default template |

**Response**

```json
{
  "id": "...",
  "org_id": "...",
  "name": "Legal Counsel System Prompt",
  "content": "You are a professional...",
  "variables": [],
  "is_default": false,
  "version": 1,
  "published_at": null,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### List Templates

```http
GET /v1/prompt-templates
```

### Get Template

```http
GET /v1/prompt-templates/{id}
```

### Update Template

```http
PATCH /v1/prompt-templates/{id}
```

**Request Body** (All fields optional)

| Field | Type |
|------|------|
| `name` | `string` |
| `content` | `string` |
| `is_default` | `bool` |

### Delete Template

```http
DELETE /v1/prompt-templates/{id}
```

---

## Agent Configs

### Create Config

```http
POST /v1/agent-configs
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | |
| `scope` | `string` | No | `org` (default) or `platform` (requires platform_admin) |
| `system_prompt_template_id` | `string` | No | Associated prompt template |
| `system_prompt_override` | `string` | No | Direct system prompt override |
| `model` | `string` | No | Default model |
| `temperature` | `float64` | No | |
| `max_output_tokens` | `int` | No | |
| `top_p` | `float64` | No | |
| `context_window_limit` | `int` | No | |
| `tool_policy` | `string` | No | `allowlist`/`denylist`/`none` |
| `tool_allowlist` | `[]string` | No | List of allowed tools |
| `tool_denylist` | `[]string` | No | List of denied tools |
| `content_filter_level` | `string` | No | |
| `project_id` | `string` | No | Associated project |
| `persona_id` | `string` | No | Associated persona |
| `is_default` | `bool` | No | Whether this is the default configuration |
| `prompt_cache_control` | `string` | No | `none` (default) or `system_prompt` |

**Response**

```json
{
  "id": "...",
  "org_id": "...",
  "scope": "org",
  "name": "Standard Legal Counsel",
  "system_prompt_template_id": "...",
  "system_prompt_override": null,
  "model": "claude-3-5-sonnet-20241022",
  "temperature": null,
  "max_output_tokens": null,
  "top_p": null,
  "context_window_limit": null,
  "tool_policy": "none",
  "tool_allowlist": [],
  "tool_denylist": [],
  "content_filter_level": "",
  "safety_rules_json": null,
  "project_id": null,
  "persona_id": null,
  "is_default": false,
  "prompt_cache_control": "none",
  "created_at": "2024-01-01T00:00:00Z"
}
```

### List Configs

```http
GET /v1/agent-configs
```

### Get Config

```http
GET /v1/agent-configs/{id}
```

### Update Config

```http
PATCH /v1/agent-configs/{id}
```

**Request Body** — All fields optional, only provided fields will be updated.

### Delete Config

```http
DELETE /v1/agent-configs/{id}
```
