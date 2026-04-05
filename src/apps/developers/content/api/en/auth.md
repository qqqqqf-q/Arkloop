---
title: "Auth"
---
## Get Captcha Configuration

```http
GET /v1/auth/captcha-config
```

No authentication required. Returns the Cloudflare Turnstile site configuration (used during front-end captcha rendering).

**Response**

```json
{
  "enabled": true,
  "site_key": "0x..."
}
```

---

## Check if User Exists

```http
POST /v1/auth/check
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `login` | `string` | Username or email |

**Response**

```json
{
  "exists": true
}
```

---

## Query Registration Mode

```http
GET /v1/auth/registration-mode
```

**Response**

```json
{
  "mode": "open"
}
```

`mode` values: `open` (open), `invite_only` (invitation only), `disabled` (closed).

---

## Register

```http
POST /v1/auth/register
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `login` | `string` | Yes | Username |
| `password` | `string` | Yes | Password |
| `email` | `string` | Yes | Email |
| `invite_code` | `string` | Conditional | Required in invitation-only mode |
| `locale` | `string` | No | Language preference |
| `cf_turnstile_token` | `string` | Conditional | Required when Turnstile is enabled |

**Response**

```json
{
  "user_id": "...",
  "access_token": "...",
  "token_type": "bearer",
  "warning": null
}
```

On success, the server issues the Refresh Token via `Set-Cookie` (HttpOnly cookie: `arkloop_refresh_token`).

---

## Login

```http
POST /v1/auth/login
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `login` | `string` | Yes | Username or email |
| `password` | `string` | Yes | Password |
| `cf_turnstile_token` | `string` | Conditional | Required when Turnstile is enabled |

**Response**

```json
{
  "access_token": "...",
  "token_type": "bearer"
}
```

On success, the server issues the Refresh Token via `Set-Cookie` (HttpOnly cookie: `arkloop_refresh_token`).

---

## Refresh Token

```http
POST /v1/auth/refresh
```

**Notes**

No request body is required. The server reads and rotates the Refresh Token from the HttpOnly cookie `arkloop_refresh_token`.

**Response** — Same format as login response (only `access_token`, `token_type`), and the Refresh Token cookie will be updated.

---

## Logout

```http
POST /v1/auth/logout
```

Requires Bearer Token. Invalidates the current token and clears the Refresh Token cookie.

**Response**

```json
{ "ok": true }
```

---

## Email Verification — Send Verification Email

```http
POST /v1/auth/email/verify/send
```

**No request body**. Uses the current logged-in user's email.

---

## Email Verification — Confirm

```http
POST /v1/auth/email/verify/confirm
```

**Request Body**

| Field | Type | Required |
|------|------|------|
| `token` | `string` | Yes |

---

## Email OTP Login — Send OTP

```http
POST /v1/auth/email/otp/send
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `email` | `string` | Yes | Target email |
| `cf_turnstile_token` | `string` | Conditional | |

---

## Email OTP Login — Verify OTP

```http
POST /v1/auth/email/otp/verify
```

**Request Body**

| Field | Type | Required |
|------|------|------|
| `email` | `string` | Yes |
| `otp` | `string` | Yes |

**Response** — Same format as login response (includes `access_token`, `token_type`), and the Refresh Token cookie will be updated.
