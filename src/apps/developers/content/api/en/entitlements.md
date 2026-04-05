---
title: "Entitlements"
---
The entitlement system controls functional access and resource quotas for organizations. Requires `platform_admin` permission.

## List Entitlement Overrides

```http
GET /v1/entitlement-overrides?org_id={org_id}
```

**Response**

```json
[
  {
    "id": "...",
    "org_id": "...",
    "key": "credits.monthly_grant",
    "value": "5000",
    "value_type": "integer",
    "reason": "Special approval for enterprise customer",
    "expires_at": null,
    "created_by_user_id": "...",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

---

## Create Entitlement Override

```http
POST /v1/entitlement-overrides
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `org_id` | `string` | Yes | Target organization |
| `key` | `string` | Yes | Entitlement key |
| `value` | `string` | Yes | Entitlement value |
| `value_type` | `string` | Yes | `integer`/`boolean`/`string` |
| `reason` | `string` | No | Reason for the operation |
| `expires_at` | `string` | No | Expiration time (RFC3339) |

---

## Delete Entitlement Override

```http
DELETE /v1/entitlement-overrides/{id}?org_id={org_id}
```

**Response**

```json
{ "ok": true }
```
