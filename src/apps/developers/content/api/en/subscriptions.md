---
title: "Subscriptions & Plans"
---
## Plans

### List Plans

```http
GET /v1/plans
```

**Response**

```json
[
  {
    "id": "...",
    "name": "starter",
    "display_name": "Starter",
    "created_at": "2024-01-01T00:00:00Z",
    "entitlements": [
      {
        "id": "...",
        "key": "credits.monthly_grant",
        "value": "1000",
        "value_type": "integer"
      }
    ]
  }
]
```

### Get Plan

```http
GET /v1/plans/{id}
```

### Create Plan

```http
POST /v1/plans
```

Requires `platform_admin` permission.

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | Internal name (unique) |
| `display_name` | `string` | Yes | Display name |
| `entitlements` | `array` | No | Initial entitlements list |

**Entitlement Object**

| Field | Type | Description |
|------|------|------|
| `key` | `string` | Entitlement key |
| `value` | `string` | Entitlement value |
| `value_type` | `string` | `integer`/`boolean`/`string` |

---

## Subscriptions

### Create Subscription

```http
POST /v1/subscriptions
```

Requires `platform_admin` permission.

**Request Body**

| Field | Type | Required |
|------|------|------|
| `org_id` | `string` | Yes |
| `plan_id` | `string` | Yes |
| `current_period_start` | `string` | Yes |
| `current_period_end` | `string` | Yes |

**Response**

```json
{
  "id": "...",
  "org_id": "...",
  "plan_id": "...",
  "status": "active",
  "current_period_start": "2024-01-01T00:00:00Z",
  "current_period_end": "2024-02-01T00:00:00Z",
  "cancelled_at": null,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### List Subscriptions

```http
GET /v1/subscriptions
```

### Get Subscription

```http
GET /v1/subscriptions/{id}
```

### Cancel Subscription

```http
DELETE /v1/subscriptions/{id}
```
