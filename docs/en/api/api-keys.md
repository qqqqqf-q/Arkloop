# API Keys

Programmatic access keys used for server-side or script-based API calls (no user login required).

All endpoints require Bearer Token authentication.

## Create API Key

```http
POST /v1/api-keys
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | Display name |
| `scopes` | `[]string` | No | Permission scope (defaults to inheriting creator's permissions) |

**Response** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "user_id": "...",
  "name": "CI Bot",
  "key_prefix": "al_xxxx",
  "key": "al_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "scopes": [],
  "revoked_at": null,
  "last_used_at": null,
  "created_at": "2024-01-01T00:00:00Z"
}
```

> The `key` field is returned only once at creation time. Please store it securely.

---

## List API Keys

```http
GET /v1/api-keys
```

**Response** (does not include plaintext `key`)

```json
[
  {
    "id": "...",
    "org_id": "...",
    "user_id": "...",
    "name": "CI Bot",
    "key_prefix": "al_xxxx",
    "scopes": [],
    "revoked_at": null,
    "last_used_at": "2024-01-15T10:00:00Z",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

---

## Revoke API Key

```http
DELETE /v1/api-keys/{id}
```

Responds with `204 No Content`. The key becomes invalid immediately after revocation.
