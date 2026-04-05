---
title: "Audit Logs"
---
All endpoints require Bearer Token authentication.

## Query Audit Logs

```http
GET /v1/audit-logs
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `org_id` | `string` | Filter by organization (platform_admin can query across organizations) |
| `action` | `string` | Action type, e.g., `user.login`, `thread.create` |
| `actor_user_id` | `string` | Actor user ID |
| `target_type` | `string` | Target resource type |
| `since` | `string` | Start time (RFC3339) |
| `until` | `string` | End time (RFC3339) |
| `limit` | `int` | Items per page |
| `offset` | `int` | Offset |
| `include` | `string` | Additional fields; `include=state` returns before/after status |

**Response**

```json
{
  "data": [
    {
      "id": "...",
      "org_id": "...",
      "actor_user_id": "...",
      "action": "user.login",
      "target_type": "user",
      "target_id": "...",
      "trace_id": "...",
      "metadata": {},
      "ip_address": "1.2.3.4",
      "user_agent": "Mozilla/5.0...",
      "created_at": "2024-01-01T00:00:00Z",
      "before_state": null,
      "after_state": null
    }
  ],
  "total": 1250
}
```

> `before_state` / `after_state` are only returned when `include=state` is provided.
