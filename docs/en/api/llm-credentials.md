# LLM Credentials

Manage LLM provider credentials and routing rules. All endpoints require Bearer Token authentication.

## Create Credential

```http
POST /v1/llm-credentials
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | Display name |
| `provider` | `string` | Yes | `openai` or `anthropic` |
| `api_key` | `string` | Yes | API key (stored encrypted) |
| `base_url` | `string` | No | Custom endpoint (third-party compatible) |
| `openai_api_mode` | `string` | No | `auto`/`responses`/`chat_completions` |
| `advanced_json` | `object` | No | Extra configuration |
| `routes` | `array` | No | Initial routing rules (see below) |

**Route Object (`routes[*]`)**

| Field | Type | Required | Description |
|------|------|------|------|
| `model` | `string` | Yes | Model name |
| `is_default` | `bool` | No | Whether this is the default route |
| `priority` | `int` | No | Priority, higher values take precedence |
| `when` | `object` | No | Condition matching rules (JSON) |
| `multiplier` | `float64` | No | Cost multiplier |
| `cost_per_1k_input` | `float64` | No | Cost per 1k input tokens (USD) |
| `cost_per_1k_output` | `float64` | No | Cost per 1k output tokens (USD) |
| `cost_per_1k_cache_write` | `float64` | No | Cache write cost |
| `cost_per_1k_cache_read` | `float64` | No | Cache read cost |

**Response** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "provider": "anthropic",
  "name": "Main Anthropic Account",
  "key_prefix": "sk-ant-a",
  "base_url": null,
  "openai_api_mode": null,
  "advanced_json": null,
  "created_at": "2024-01-01T00:00:00Z",
  "routes": [
    {
      "id": "...",
      "credential_id": "...",
      "model": "claude-3-5-sonnet-20241022",
      "priority": 0,
      "is_default": true,
      "when": null,
      "multiplier": 1.0,
      "cost_per_1k_input": 0.003,
      "cost_per_1k_output": 0.015,
      "cost_per_1k_cache_write": null,
      "cost_per_1k_cache_read": null
    }
  ]
}
```

---

## List Credentials

```http
GET /v1/llm-credentials
```

---

## Update Credential

```http
PATCH /v1/llm-credentials/{cred_id}
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `name` | `string` | |
| `provider` | `string` | |
| `base_url` | `string \| null` | |
| `openai_api_mode` | `string \| null` | |
| `api_key` | `string` | Overwrites existing key if provided |
| `advanced_json` | `object` | |

---

## Delete Credential

```http
DELETE /v1/llm-credentials/{cred_id}
```

---

## Copy Credential

```http
POST /v1/llm-credentials/{cred_id}/copy
```

Copies the specified credential and all its routes with the following behavior:

- The new credential name is automatically appended with a `-N` suffix (e.g., `main-openai` -> `main-openai-1`, increments on repeated copies).
- Reuses the original API Key (no need to re-enter).
- Copied routes will generate new `route_id`s.

**Response** `201 Created`

Returns the same structure as "Create Credential" (including `routes`).

---

## Update Route

```http
PATCH /v1/llm-credentials/{cred_id}/routes/{route_id}
```

**Request Body** — Same format as the route object in Create Credential.
