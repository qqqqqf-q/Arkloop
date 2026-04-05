---
title: "Current User (Me)"
---
All endpoints require Bearer Token authentication.

## Get Current User Info

```http
GET /v1/me
```

**Response**

```json
{
  "id": "...",
  "username": "alice",
  "email": "alice@example.com",
  "email_verified": true,
  "email_verification_required": false,
  "created_at": "2024-01-01T00:00:00Z",
  "org_id": "...",
  "org_name": "Acme Corp",
  "role": "admin",
  "permissions": ["data.threads.read", "data.threads.manage"]
}
```

| Field | Type | Description |
|------|------|------|
| `permissions` | `[]string` | List of permission points for the current user in their organization |

---

## Update Current User

```http
PATCH /v1/me
```

**Request Body**

| Field | Type |
|------|------|
| `username` | `string` |

**Response**

```json
{ "username": "new_name" }
```

---

## Usage Statistics (Monthly)

```http
GET /v1/me/usage
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `year` | `int` | Optional, defaults to current month |
| `month` | `int` | Optional, defaults to current month |

**Response**

```json
{
  "org_id": "...",
  "year": 2024,
  "month": 1,
  "total_input_tokens": 100000,
  "total_output_tokens": 50000,
  "total_cost_usd": 0.15,
  "record_count": 42
}
```

---

## Usage Statistics (Daily)

```http
GET /v1/me/usage/daily
```

**Query Parameters**

| Parameter | Type | Required | Description |
|------|------|------|------|
| `start` | `string` | Yes | Start date, format `YYYY-MM-DD` |
| `end` | `string` | Yes | End date, format `YYYY-MM-DD` |

**Response**

```json
[
  {
    "date": "2024-01-01",
    "input_tokens": 1000,
    "output_tokens": 500,
    "cost_usd": 0.01,
    "record_count": 3
  }
]
```

---

## Usage Statistics (by Model)

```http
GET /v1/me/usage/by-model
```

**Query Parameters** — Same as monthly statistics.

**Response**

```json
[
  {
    "model": "claude-3-5-sonnet",
    "input_tokens": 50000,
    "output_tokens": 25000,
    "cost_usd": 0.08,
    "record_count": 20
  }
]
```

---

## Get My Credits

```http
GET /v1/me/credits
```

**Response**

```json
{
  "balance": 1000,
  "transactions": [
    {
      "id": "...",
      "org_id": "...",
      "amount": 100,
      "type": "credit",
      "reference_type": "admin_adjust",
      "reference_id": "...",
      "note": "Initial recharge",
      "thread_title": null,
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

---

## Get My Invitation Code

```http
GET /v1/me/invite-code
```

**Response**

```json
{
  "id": "...",
  "user_id": "...",
  "code": "ABC123",
  "max_uses": 10,
  "use_count": 2,
  "is_active": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## Reset Invitation Code

```http
POST /v1/me/invite-code/reset
```

**Response** — Same as "Get My Invitation Code".

---

## Redemption

```http
POST /v1/me/redeem
```

**Request Body**

| Field | Type | Required |
|------|------|------|
| `code` | `string` | Yes |

**Response**

```json
{
  "code": "PROMO2024",
  "type": "credits",
  "value": "100"
}
```
