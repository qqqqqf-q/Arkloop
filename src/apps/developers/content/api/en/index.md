---
title: "API Overview"
---
The Arkloop API is a RESTful HTTP API with a base path of `/v1`.

## Base URL

| Environment | URL |
|------|------|
| Local Development (Direct) | `http://127.0.0.1:19001` |
| Local Development (via Gateway) | `http://127.0.0.1:19000` |

## Authentication

All protected endpoints require a Bearer Token in the request header:

```http
Authorization: Bearer <access_token>
```

Tokens are obtained via [`POST /v1/auth/login`](./auth#login). After expiration, use the Refresh Token to obtain a new token ([`POST /v1/auth/refresh`](./auth#refresh-token)).
After login, the Refresh Token is stored in an HttpOnly cookie by the server; the front-end refreshes the Access Token by calling the refresh endpoint.

API Key authentication is also supported for some endpoints:

```http
Authorization: Bearer al_...
```

## Error Response Format

```json
{
  "error": "error.code",
  "message": "human-readable description",
  "trace_id": "...",
  "details": {}
}
```

Common error codes:

| HTTP Status Code | Error Code | Description |
|------------|--------|------|
| 400 | `validation.error` | Request parameter validation failed |
| 401 | `auth.unauthorized` | Unauthorized or invalid token |
| 403 | `auth.forbidden` | Insufficient permissions |
| 404 | `not_found` | Resource not found |
| 409 | `conflict` | Resource conflict (e.g., duplicate creation) |
| 422 | `validation.error` | Request body parsing failed |
| 429 | `rate_limit` | Request frequency limit exceeded |
| 500 | `internal_error` | Internal server error |

## Pagination

List endpoints that support pagination use cursor-based pagination:

```
GET /v1/threads?limit=20&before=<cursor>
```

## SSE (Server-Sent Events)

Run execution progress is pushed via SSE events. For details, see [Run Execution Endpoints](./runs#sse-event-stream).

## Endpoint Index

### Auth & Account
- [Auth](./auth) — Login, registration, token refresh, email verification
- [Me](./me) — Personal info, usage, credits, invitation codes
- [API Keys](./api-keys) — Programmatic access keys

### Core Resources
- [Threads](./threads) — session management
- [Messages](./messages) — message reading and writing
- [Runs](./runs) — Agent Loop execution
- [Projects](./projects) — session grouping

### Accounts
- [Accounts](./accounts) — personal and workspace accounts, memberships

### Configuration
- [LLM Providers](./llm-providers) — provider accounts, model lists, and selector prefixes
- [MCP Configs](./mcp-configs) — MCP server configuration
- [Tool Providers](./tool-providers) — tool backend and credential configuration
- [ASR Credentials](./asr-credentials) — speech recognition credentials

### Billing & Entitlements
- [Credits & Usage](./credits) — credit management and usage statistics
- [Subscriptions & Plans](./subscriptions) — subscriptions and plans
- [Entitlements](./entitlements) — entitlement coverage
- [API Keys](./api-keys) — API key management

### Notifications & Webhooks
- [Notifications](./notifications) — in-app notifications
- [Webhooks](./webhooks) — event delivery

### Admin
- [Admin Overview](./admin) — summary of platform management endpoints
- [Audit Logs](./audit-logs) — operation auditing
- [IP Rules](./ip-rules) — access control
- [Feature Flags](./feature-flags) — feature toggles

### System
- [Health Check](./health) — `/healthz` `/readyz`
