---
title: "Credits & Usage"
---
## Get My Credits

See [Current User — Get My Credits](./me#get-my-credits).

---

## Organization Usage Statistics

See [Organizations — Usage Statistics](./orgs#get-monthly-org-usage).

---

## Admin: Query Credits

```http
GET /v1/admin/credits?org_id={org_id}
```

Requires `platform_admin` permission.

**Response**

```json
{
  "org_id": "...",
  "balance": 5000,
  "transactions": [...]
}
```

---

## Admin: Adjust Credits

```http
POST /v1/admin/credits/adjust
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `org_id` | `string` | Yes | Target Organization ID |
| `amount` | `int64` | Yes | Adjustment amount (positive to add, negative to subtract) |
| `note` | `string` | No | Remark |

**Response**

```json
{
  "org_id": "...",
  "balance": 5100
}
```

---

## Admin: Bulk Adjust Credits (All Organizations)

```http
POST /v1/admin/credits/bulk-adjust
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `amount` | `int64` | Yes | Adjustment amount |
| `note` | `string` | No | Remark |

**Response**

```json
{ "affected": 42 }
```

---

## Admin: Reset All Organization Credits

```http
POST /v1/admin/credits/reset-all
```

**Request Body**

| Field | Type |
|------|------|
| `note` | `string` |

**Response**

```json
{ "affected": 42 }
```

---

## Admin: Platform Usage Statistics

### By Day

```http
GET /v1/admin/usage/daily?start=YYYY-MM-DD&end=YYYY-MM-DD
```

### Monthly Summary

```http
GET /v1/admin/usage/summary?year=2024&month=1
```

### By Model

```http
GET /v1/admin/usage/by-model?year=2024&month=1
```
