---
title: "Admin Overview"
---
Admin endpoints require `platform_admin` permission.

## Dashboard Statistics

```http
GET /v1/admin/dashboard
```

**Response**

```json
{
  "total_users": 1200,
  "active_users_30d": 380,
  "total_runs": 45000,
  "runs_today": 230,
  "total_input_tokens": 150000000,
  "total_output_tokens": 75000000,
  "total_cost_usd": 450.00,
  "active_orgs": 85
}
```

---

## User Management

### List Users

```http
GET /v1/admin/users
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `q` | `string` | Keyword search (username/email) |
| `status` | `string` | User status filter |
| `limit` | `int` | |
| `cursor` | `string` | Pagination cursor |

**Response**

```json
[
  {
    "id": "...",
    "login": "alice",
    "username": "Alice",
    "email": "alice@example.com",
    "status": "active",
    "avatar_url": null,
    "locale": "zh-CN",
    "timezone": "Asia/Shanghai",
    "last_login_at": "2024-01-15T10:00:00Z",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

### Get User Details

```http
GET /v1/admin/users/{user_id}
```

Response includes basic user information and a list of organizations they belong to.

### Update User

```http
PATCH /v1/admin/users/{user_id}
```

**Request Body** (All fields optional)

| Field | Type | Description |
|------|------|------|
| `status` | `string` | User status |
| `username` | `string` | |
| `email` | `string` | |
| `email_verified` | `bool` | |
| `locale` | `string` | |
| `timezone` | `string` | |

### Delete User

```http
DELETE /v1/admin/users/{user_id}
```

Responds with `204 No Content`.

---

## Run Details

```http
GET /v1/admin/runs/{run_id}
```

**Response** (Contains full execution details)

```json
{
  "run_id": "...",
  "org_id": "...",
  "thread_id": "...",
  "status": "completed",
  "model": "claude-3-5-sonnet-20241022",
  "persona_id": null,
  "provider_kind": "anthropic",
  "credential_name": "Main Anthropic Account",
  "persona_model": "anthropic-main^claude-sonnet-4-20250514",
  "duration_ms": 3200,
  "total_input_tokens": 1000,
  "total_output_tokens": 500,
  "total_cost_usd": 0.005,
  "created_at": "2024-01-01T00:00:00Z",
  "completed_at": "2024-01-01T00:00:03Z"
}
```

---

## Email Configuration

### Get Email Status

```http
GET /v1/admin/email/status
```

**Response**

```json
{
  "configured": true,
  "from": "noreply@example.com",
  "source": "db"
}
```

`source` values: `db` (database configuration), `env` (environment variables), `none` (not configured).

### Get Email Configuration

```http
GET /v1/admin/email/config
```

**Response** (Does not include plaintext password)

```json
{
  "from": "noreply@example.com",
  "smtp_host": "smtp.example.com",
  "smtp_port": 587,
  "smtp_user": "noreply@example.com",
  "smtp_pass_set": true,
  "smtp_tls_mode": "starttls"
}
```

### Update Email Configuration

```http
PUT /v1/admin/email/config
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `from` | `string` | Sender's email address |
| `smtp_host` | `string` | |
| `smtp_port` | `int` | |
| `smtp_user` | `string` | |
| `smtp_pass` | `string` | |
| `smtp_tls_mode` | `string` | `none`/`starttls`/`tls` |

Responds with `204 No Content`.

### Send Test Email

```http
POST /v1/admin/email/test
```

**Request Body**

| Field | Type | Required |
|------|------|------|
| `to` | `string` | Yes |

Responds with `204 No Content`.

---

## Gateway Configuration

### Get Gateway Configuration

```http
GET /v1/admin/gateway-config
```

**Response**

```json
{
  "ip_mode": "proxy",
  "trusted_cidrs": ["10.0.0.0/8"],
  "risk_reject_threshold": 0.8
}
```

### Update Gateway Configuration

```http
PUT /v1/admin/gateway-config
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `ip_mode` | `string` | `direct`/`proxy`/`cdn` |
| `trusted_cidrs` | `[]string` | Trusted CIDR list |
| `risk_reject_threshold` | `float64` | Risk rejection threshold (0-1) |

---

## Access Log

```http
GET /v1/admin/access-log
```

Real-time access log query based on Redis Stream.

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `limit` | `int` | Items per page |
| `before` | `string` | cursor (forward pagination) |
| `since` | `string` | Starting from this ID |
| `method` | `string` | HTTP method filter |
| `path` | `string` | Path prefix filter |
| `ip` | `string` | IP filter |
| `country` | `string` | Country code filter |
| `risk_min` | `float64` | Minimum risk score |
| `ua_type` | `string` | UA type filter |

**Response**

```json
{
  "data": [
    {
      "id": "...",
      "timestamp": "2024-01-01T00:00:00Z",
      "trace_id": "...",
      "method": "POST",
      "path": "/v1/threads",
      "status_code": 201,
      "duration_ms": 45,
      "client_ip": "1.2.3.4",
      "country": "CN",
      "city": "Beijing",
      "user_agent": "Mozilla/5.0...",
      "ua_type": "browser",
      "risk_score": 0.1,
      "identity_type": "user",
      "org_id": "...",
      "user_id": "...",
      "username": "alice"
    }
  ],
  "has_more": true,
  "next_before": "..."
}
```

---

## Invitation Code Management

### List Invitation Codes

```http
GET /v1/admin/invite-codes
```

**Query Parameters**: `limit`, `q` (keyword), `cursor`

**Response**

```json
[
  {
    "id": "...",
    "user_id": "...",
    "code": "ABC123",
    "max_uses": 10,
    "use_count": 3,
    "is_active": true,
    "created_at": "...",
    "user_login": "alice",
    "user_email": "alice@example.com"
  }
]
```

### Get Invitation Code

```http
GET /v1/admin/invite-codes/{id}
```

### Update Invitation Code

```http
PATCH /v1/admin/invite-codes/{id}
```

**Request Body**

| Field | Type |
|------|------|
| `max_uses` | `int` |
| `is_active` | `bool` |

---

## Referral Relationship

### List Referral Records

```http
GET /v1/admin/referrals
```

**Query Parameters**: `inviter_user_id`, `limit`, `cursor`

### Get Referral Tree

```http
GET /v1/admin/referrals/tree?user_id={user_id}
```

**Response**

```json
[
  {
    "user_id": "...",
    "login": "alice",
    "inviter_id": null,
    "depth": 0,
    "created_at": "..."
  }
]
```

---

## Redemption Code Management

### Bulk Create Redemption Codes

```http
POST /v1/admin/redemption-codes/batch
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `count` | `int` | Yes | Number to generate |
| `type` | `string` | Yes | Type (e.g., `credits`) |
| `value` | `string` | Yes | Redemption value |
| `max_uses` | `int` | No | Maximum uses per code |
| `expires_at` | `string` | No | Expiration time (RFC3339) |
| `batch_id` | `string` | No | Batch identifier |

### List Redemption Codes

```http
GET /v1/admin/redemption-codes
```

**Query Parameters**: `limit`, `q`, `type`, `cursor`

### Update Redemption Code

```http
PATCH /v1/admin/redemption-codes/{id}
```

**Request Body**

| Field | Type |
|------|------|
| `is_active` | `bool` |
