---
title: "LLM Providers"
---
Manage provider accounts and the model list under each provider. All endpoints require Bearer Token authentication.

A provider is an account-level resource. Models are nested under the provider and are used to build model selectors such as `provider_name^model_name`.

## List Providers

```http
GET /v1/llm-providers
```

**Response** `200 OK`

```json
[
  {
    "id": "...",
    "org_id": "...",
    "provider": "anthropic",
    "name": "anthropic-main",
    "key_prefix": "sk-ant-a",
    "base_url": null,
    "openai_api_mode": null,
    "advanced_json": null,
    "created_at": "2024-01-01T00:00:00Z",
    "models": [
      {
        "id": "...",
        "provider_id": "...",
        "model": "claude-sonnet-4-20250514",
        "priority": 0,
        "is_default": true,
        "tags": [],
        "when": {},
        "advanced_json": {},
        "multiplier": 1.0,
        "cost_per_1k_input": 0.003,
        "cost_per_1k_output": 0.015,
        "cost_per_1k_cache_write": null,
        "cost_per_1k_cache_read": null
      }
    ]
  }
]
```

---

## Create Provider

```http
POST /v1/llm-providers
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | Provider display name, also the selector prefix; must not contain `^` |
| `provider` | `string` | Yes | `openai` or `anthropic` |
| `api_key` | `string` | Yes | API key (stored encrypted) |
| `base_url` | `string` | No | Custom endpoint |
| `openai_api_mode` | `string` | No | `auto` / `responses` / `chat_completions` |
| `advanced_json` | `object` | No | Extra provider configuration |

---

## Update Provider

```http
PATCH /v1/llm-providers/{provider_id}
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `name` | `string` | Update provider name |
| `provider` | `string` | Update provider kind |
| `api_key` | `string` | Overwrite stored key |
| `base_url` | `string \| null` | Update custom endpoint |
| `openai_api_mode` | `string \| null` | Update OpenAI compatibility mode |
| `advanced_json` | `object \| null` | Update advanced configuration |

---

## Delete Provider

```http
DELETE /v1/llm-providers/{provider_id}
```

Deleting a provider also removes its nested model list.

---

## Create Provider Model

```http
POST /v1/llm-providers/{provider_id}/models
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `model` | `string` | Yes | Upstream model name |
| `priority` | `int` | No | Higher wins first |
| `is_default` | `bool` | No | Whether this is the default model under the provider |
| `tags` | `string[]` | No | Extra tags |
| `when` | `object` | No | Conditional routing rules |
| `advanced_json` | `object` | No | Model-level advanced config; merged over provider-level keys |
| `multiplier` | `float64` | No | Cost multiplier |
| `cost_per_1k_input` | `float64` | No | Cost per 1k input tokens |
| `cost_per_1k_output` | `float64` | No | Cost per 1k output tokens |
| `cost_per_1k_cache_write` | `float64` | No | Cache write cost |
| `cost_per_1k_cache_read` | `float64` | No | Cache read cost |

A Persona can later reference the model with a selector like `anthropic-main^claude-sonnet-4-20250514`.

---

## Update Provider Model

```http
PATCH /v1/llm-providers/{provider_id}/models/{model_id}
```

Updates the same fields as Create Provider Model.

When both provider-level and model-level `advanced_json` are set, Arkloop merges them by key and the model-level value wins on conflicts.

---

## Delete Provider Model

```http
DELETE /v1/llm-providers/{provider_id}/models/{model_id}
```

---

## List Available Models

```http
GET /v1/llm-providers/{provider_id}/available-models
```

Returns the upstream-discovered model list for the selected provider:

```json
{
  "models": [
    {
      "id": "claude-sonnet-4-20250514",
      "name": "claude-sonnet-4-20250514",
      "configured": true
    }
  ]
}
```
