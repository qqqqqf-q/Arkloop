---
title: "Tool Providers"
---
Manage backend providers, credentials, and base URLs for built-in Tool Groups (e.g., `web_search` / `web_fetch`).

Tool Provider configuration is divided into two layers:
- `scope=platform`: Platform-wide global default (suitable for self-hosting/open-source; new organizations can use directly without configuration).
- `scope=org`: Organization override (tenant-specific customization).

## List Tool Providers

```http
GET /v1/tool-providers?scope=platform
```

`scope`:
- `platform` (default): Requires `platform_admin`.
- `org`: Requires `data.secrets` permission within the organization (used to manage sensitive credentials).

**Response** `200 OK`

```json
{
  "groups": [
    {
      "group_name": "web_search",
      "providers": [
        {
          "group_name": "web_search",
          "provider_name": "web_search.tavily",
          "is_active": true,
          "key_prefix": "tvly-****1234",
          "requires_api_key": true,
          "requires_base_url": false,
          "configured": true
        },
        {
          "group_name": "web_search",
          "provider_name": "web_search.searxng",
          "is_active": false,
          "base_url": null,
          "requires_api_key": false,
          "requires_base_url": true,
          "configured": false
        }
      ]
    }
  ]
}
```

Field Descriptions:
- `is_active`: Whether it is active within the current scope (at most one active per scope + group).
- `configured`: Whether the required fields for the provider (API Key / Base URL) are satisfied.
- `key_prefix`: Used for display only; does not return the plaintext key.

## Activate Provider

```http
PUT /v1/tool-providers/{group}/{provider}/activate?scope=platform
```

Behavior:
- Atomically switches the active provider within the same `scope + group`.
- Returns `204 No Content` on success.

## Deactivate Provider

```http
PUT /v1/tool-providers/{group}/{provider}/deactivate?scope=platform
```

Returns `204 No Content` on success.

## Write/Update Credentials and Base URL

```http
PUT /v1/tool-providers/{group}/{provider}/credential?scope=platform
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `api_key` | `string` | No | Overwrites existing key if provided (stored encrypted) |
| `base_url` | `string` | No | Overwrites existing base_url if provided (trailing `/` automatically removed) |

Notes:
- `web_search.searxng` must provide `base_url`.
- `web_search.tavily` / `web_fetch.jina` / `web_fetch.firecrawl` must provide `api_key`.
- If both `api_key` and `base_url` are missing, the endpoint returns `204` and makes no changes.

Returns `204 No Content` on success.

## Clear Credentials

```http
DELETE /v1/tool-providers/{group}/{provider}/credential?scope=platform
```

Clearing Behavior:
- Deletes the corresponding `secret` (within the same scope).
- Unbinds `tool_provider_configs.secret_id` and `key_prefix`.

Returns `204 No Content` on success.

## Runtime Resolution Priority (Worker)

`web_search` / `web_fetch` only expose the Tool Group name to the LLM. The Worker resolves to the final Provider based on the following priority:

1) org scope active provider  
2) platform scope active provider  
3) legacy group executor (Config Resolver's `web_search.*` / `web_fetch.*`)  
4) otherwise returns `tool.not_configured`
