---
title: "MCP Configs"
---
Manage MCP (Model Context Protocol) server configurations. All endpoints require Bearer Token authentication.

## Create MCP Config

```http
POST /v1/mcp-configs
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | Display name |
| `transport` | `string` | Yes | `stdio`/`http_sse`/`streamable_http` |
| `url` | `string` | Conditional | Required when `transport != stdio` |
| `bearer_token` | `string` | No | HTTP authentication token (stored encrypted) |
| `command` | `string` | Conditional | Required when `transport == stdio` |
| `args` | `[]string` | No | Command line arguments |
| `cwd` | `string` | No | Working directory |
| `env` | `object` | No | Extra environment variables |
| `inherit_parent_env` | `bool` | No | Whether to inherit parent process environment variables |
| `call_timeout_ms` | `int` | No | Call timeout (ms), default `10000` |

**Response** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "name": "Filesystem Tools",
  "transport": "stdio",
  "url": null,
  "has_auth": false,
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
  "cwd": null,
  "inherit_parent_env": false,
  "call_timeout_ms": 10000,
  "is_active": true,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

> `has_auth` being `true` indicates that `bearer_token` is configured, but the plaintext token is not returned in the response.

---

## List MCP Configs

```http
GET /v1/mcp-configs
```

---

## Update MCP Config

```http
PATCH /v1/mcp-configs/{config_id}
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `name` | `string` | |
| `url` | `string \| null` | |
| `bearer_token` | `string \| null` | `null` to clear the token |
| `call_timeout_ms` | `int` | |
| `is_active` | `bool` | Enable/Disable |

---

## Delete MCP Config

```http
DELETE /v1/mcp-configs/{config_id}
```
