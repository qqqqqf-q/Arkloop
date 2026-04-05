---
title: "IP Rules"
---
Manage organization-level IP access control. Requires appropriate permissions.

## Create IP Rule

```http
POST /v1/ip-rules
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `type` | `string` | Yes | `allowlist` or `blocklist` |
| `cidr` | `string` | Yes | CIDR format, e.g., `192.168.1.0/24` |
| `note` | `string` | No | Remark |

**Response** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "type": "allowlist",
  "cidr": "10.0.0.0/8",
  "note": "Intranet access",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## List IP Rules

```http
GET /v1/ip-rules
```

---

## Delete IP Rule

```http
DELETE /v1/ip-rules/{rule_id}
```

Responds with `204 No Content`.
