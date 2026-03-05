# Backend API and SSE Specifications

This document describes the resource model, endpoint design, error model, and SSE event specifications for the Arkloop API layer. The API is implemented in Go, based on the `net/http` standard library.

## 1. Core Principles

- **API as Control Plane Only**: Handles authentication, resource orchestration, audit logging, SSE playback, and job enqueuing. Tool execution occurs in the Worker.
- **Events as the Single Source of Truth**: The execution process is expressed through `run_events`. SSE push, database storage, and playback are based on the same event source.
- **Streaming First**: Model outputs and tool invocation processes are uniformly pushed via SSE.
- **Multi-tenant Isolation**: All write operations belong to an `org_id`; data visibility is controlled by RBAC and membership relations.
- **Streaming Authentication for Fetch**: SSE and standard APIs use the same `Authorization: Bearer` mechanism, consistent across Web and CLI.

## 2. Resource Model

### Core Resources

| Resource | Description |
|------|------|
| `orgs` | Tenant boundary (data isolation, billing, auditing) |
| `users` | User entity |
| `org_memberships` | Organization membership and roles |
| `teams` | Groups within an organization |
| `projects` | Projects/Collaboration domains (can be associated with a team) |
| `threads` | Conversation containers (supports soft delete) |
| `messages` | User/Assistant messages (`content_json` JSONB) |
| `runs` | Agent Loop execution instances (supports `parent_run_id` for sub-runs) |
| `run_events` | Event stream (partitioned by month, `seq` monotonically increasing) |

### Configuration Resources

| Resource | Description |
|------|------|
| `llm_credentials` | LLM provider credentials (stored with AES-256-GCM encryption) |
| `llm_routes` | Model routing rules (credential + model + priority + multiplier) |
| `asr_credentials` | Speech-to-text credentials |
| `mcp_configs` | MCP server configurations (stdio/HTTP types) |
| `personas` | Persona definitions (executor_type + prompt + tool_allowlist) |
| `agent_configs` | Agent configurations (system_prompt, reasoning_mode, model routing) |
| `prompt_templates` | Prompt templates |

### Enterprise Resources

| Resource | Description |
|------|------|
| `api_keys` | External access keys (stored as hashes) |
| `ip_rules` | IP access rules |
| `webhook_endpoints` | Webhook endpoints |
| `plans` / `subscriptions` | Subscription plan system |
| `credits` / `credit_transactions` | Credits/Quota system |
| `entitlement_overrides` | Organization-level feature permission overrides |
| `audit_logs` | Audit logs |
| `notifications` / `notification_broadcasts` | Notification system |
| `user_memory_snapshots` | User memory snapshots (OpenViking) |

## 3. Endpoint Design

### 3.1 Health Checks

- `GET /healthz` -- Liveness probe
- `GET /readyz` -- Readiness probe (includes schema version validation)

### 3.2 Authentication and Sessions

- `POST /v1/auth/register` -- Register
- `POST /v1/auth/login` -- Login
- `POST /v1/auth/refresh` -- Refresh token
- `POST /v1/auth/logout` -- Logout
- `POST /v1/auth/check` -- Check authentication status
- `GET /v1/auth/registration-mode` -- Query registration mode
- `GET /v1/auth/captcha-config` -- Captcha configuration (Cloudflare Turnstile)
- `POST /v1/auth/email/verify/send` -- Send email verification
- `POST /v1/auth/email/verify/confirm` -- Confirm email verification
- `POST /v1/auth/email/otp/send` -- Send OTP
- `POST /v1/auth/email/otp/verify` -- Verify OTP

### 3.3 User

- `GET /v1/me` -- Current user information
- `GET /v1/me/usage` -- Usage statistics
- `GET /v1/me/usage/daily` -- Daily usage
- `GET /v1/me/usage/by-model` -- Usage by model
- `GET /v1/me/feedback` -- Feedback records
- `POST /v1/me/credits` -- Credit operations
- `GET /v1/me/invite-code` -- Invite code
- `POST /v1/me/invite-code/reset` -- Reset invite code
- `POST /v1/me/redeem` -- Redemption code verification

### 3.4 Threads and Messages

- `GET /v1/threads` -- List
- `POST /v1/threads` -- Create (optional `project_id`, `agent_config_id`)
- `GET /v1/threads/{id}` -- Details
- `PUT /v1/threads/{id}` -- Update
- `DELETE /v1/threads/{id}` -- Soft delete
- `GET /v1/threads/search` -- Search
- `GET /v1/threads/starred` -- Starred list
- `GET /v1/threads/{id}/messages` -- Message list
- `POST /v1/threads/{id}/messages` -- Write user message

### 3.5 Runs

A Run transforms "Input Message + Persona Configuration + Routing Strategy" into an auditable execution chain.

- `POST /v1/threads/{id}/runs` -- Create run
  - Parameters: `persona_id` (optional), `route_id` (optional), configuration overrides
  - Within the same API transaction: Write `runs` row + write `run.started` event + insert `jobs` (`run.execute`)
- `GET /v1/runs` -- List
- `GET /v1/runs/{id}` -- SSE event stream (`Content-Type: text/event-stream`)
- `POST /v1/runs/{id}/cancel` -- Cancel
- `POST /v1/runs/{id}/input` -- Submit user input (Human-in-the-loop)
- `POST /v1/runs/{id}/retry` -- Retry

SSE Conventions:
- Events monotonically increase by `seq`
- Supports `?after_seq=N` cursor for resuming after disconnection
- Heartbeat interval: 15s (`ARKLOOP_SSE_HEARTBEAT_SECONDS`)
- Batch limit: 500 (`ARKLOOP_SSE_BATCH_LIMIT`)
- Transport layer: PostgreSQL `LISTEN/NOTIFY` (direct connection via `ARKLOOP_DATABASE_DIRECT_URL`, bypassing PgBouncer)

### 3.6 Public Sharing

- `GET /v1/s/{share_id}` -- Public share access

### 3.7 LLM Credentials and Routes

- `GET/POST /v1/llm-credentials` -- Credential management
- `GET/PUT/DELETE /v1/llm-credentials/{id}`
- `GET/POST /v1/llm-routes` -- Routing rule management
- `GET/PUT/DELETE /v1/llm-routes/{id}`

### 3.8 ASR (Speech-to-Text)

- `GET/POST /v1/asr-credentials`
- `GET/PUT/DELETE /v1/asr-credentials/{id}`
- `POST /v1/asr/transcribe` -- Transcribe

### 3.9 MCP Configuration

- `GET/POST /v1/mcp-configs`
- `GET/PUT/DELETE /v1/mcp-configs/{id}`

### 3.10 Personas and Agent Configurations

Note: External naming has migrated from `skills` to `personas` (`/v1/skills` -> `/v1/personas`, `skill_key/skill_id` -> `persona_key/persona_id`). Legacy compatibility is not provided.

- `GET/POST /v1/personas`
- `GET/PUT/DELETE /v1/personas/{id}`
- `GET/POST /v1/agent-configs`
- `GET/PUT/DELETE /v1/agent-configs/{id}`
- `GET/POST /v1/prompt-templates`
- `GET/PUT/DELETE /v1/prompt-templates/{id}`

### 3.11 Organizations and Teams

- `GET/POST /v1/orgs`
- `GET /v1/orgs/me` -- Current user's organization
- `GET/POST /v1/orgs/{id}`
- `GET/POST /v1/orgs/{id}/invitations` -- Invitation management
- `GET /v1/orgs/{id}/usage` -- Organization usage
- `GET /v1/orgs/{id}/usage/daily`
- `GET /v1/orgs/{id}/usage/by-model`
- `GET/POST /v1/org-invitations`
- `GET/PUT/DELETE /v1/org-invitations/{id}`
- `GET/POST /v1/teams`
- `GET/PUT/DELETE /v1/teams/{id}`
- `GET/POST /v1/projects`
- `GET/PUT/DELETE /v1/projects/{id}`

### 3.12 Security and Access Control

- `GET/POST /v1/api-keys`
- `GET/PUT/DELETE /v1/api-keys/{id}`
- `GET/POST /v1/ip-rules`
- `GET/PUT/DELETE /v1/ip-rules/{id}`

### 3.13 Webhooks

- `GET/POST /v1/webhook-endpoints`
- `GET/PUT/DELETE /v1/webhook-endpoints/{id}`

### 3.14 Subscriptions and Billing

- `GET/POST /v1/plans`
- `GET/PUT/DELETE /v1/plans/{id}`
- `GET/POST /v1/subscriptions`
- `GET/PUT/DELETE /v1/subscriptions/{id}`
- `GET/POST /v1/entitlement-overrides`
- `GET/PUT/DELETE /v1/entitlement-overrides/{id}`

### 3.15 Notifications and Auditing

- `GET/POST /v1/notifications`
- `GET/PUT/DELETE /v1/notifications/{id}`
- `GET /v1/audit-logs`
- `GET /v1/feature-flags`
- `GET/PUT/DELETE /v1/feature-flags/{id}`

### 3.16 Artifacts

- `GET/PUT/DELETE /v1/artifacts/{id}`

### 3.17 Admin Console

Admin endpoints are prefixed with `/v1/admin/` and require platform administrator permissions.

**Dashboard and Reports:**
- `GET /v1/admin/dashboard`
- `GET /v1/admin/runs/{id}`
- `GET /v1/admin/reports`
- `GET /v1/admin/usage/daily`
- `GET /v1/admin/usage/summary`
- `GET /v1/admin/usage/by-model`
- `GET /v1/admin/access-log`

**User Management:**
- `GET/POST /v1/admin/users`
- `GET/PUT/DELETE /v1/admin/users/{id}`

**Invite Codes:**
- `GET/POST /v1/admin/invite-codes`
- `GET/PUT/DELETE /v1/admin/invite-codes/{id}`

**Referral System:**
- `GET /v1/admin/referrals`
- `GET /v1/admin/referrals/tree`

**Credit Management:**
- `GET/POST /v1/admin/credits`
- `POST /v1/admin/credits/adjust`
- `POST /v1/admin/credits/bulk-adjust`
- `POST /v1/admin/credits/reset-all`

**Redemption Codes:**
- `GET/POST /v1/admin/redemption-codes`
- `GET/PUT/DELETE /v1/admin/redemption-codes/{id}`
- `POST /v1/admin/redemption-codes/batch`

**Notification Broadcasts:**
- `GET/POST /v1/admin/notifications/broadcasts`
- `GET/PUT/DELETE /v1/admin/notifications/broadcasts/{id}`

**Platform Configuration:**
- `GET /v1/admin/gateway-config`
- `PUT /v1/admin/gateway-config/{id}`
- `GET/POST /v1/admin/platform-settings`
- `GET/PUT/DELETE /v1/admin/platform-settings/{id}`

**Email:**
- `GET /v1/admin/email/status`
- `GET /v1/admin/email/config`
- `POST /v1/admin/email/test`

## 4. SSE Event Specifications

### 4.1 Event Envelope

Shared across all events:

| Field | Description |
|------|------|
| `event_id` | Globally unique |
| `run_id` | Associated run |
| `seq` | Monotonically increasing sequence within the run |
| `ts` | Server-side timestamp |
| `type` | Event type |
| `data_json` | Event payload |

### 4.2 Event Types

**Run Lifecycle:**

| Type | Description |
|------|------|
| `run.started` | Run started |
| `run.completed` | Run completed |
| `run.failed` | Run failed (includes `error_class`) |
| `run.cancelled` | Run cancelled |
| `run.cancel_requested` | Cancellation signal received |

**Human-in-the-loop:**

| Type | Description |
|------|------|
| `run.input_requested` | Waiting for user input |
| `run.input_provided` | User submitted input |

**Message Stream:**

| Type | Description |
|------|------|
| `message.delta` | Model streaming increment (`content_delta`, `role`) |

**Tool Calls:**

| Type | Description |
|------|------|
| `tool.call` | Tool invocation initiated |
| `tool.result` | Tool execution result |
| `tool.denied` | Tool rejected by policy/resource limits |

**Agent Loop Internal:**

| Type | Description |
|------|------|
| `run.route.selected` | Provider route selected |
| `run.segment.start` | Iteration/segment start |
| `run.segment.end` | Iteration/segment end |
| `run.llm.retry` | LLM retry |
| `run.provider_fallback` | Provider fallback |

**Lua Executor Extensions:**

| Type | Description |
|------|------|
| `agent.parallel_dispatch` | Parallel execution dispatch |
| `agent.parallel_complete` | Parallel execution completion |

**Debug Events (enabled via `ARKLOOP_LLM_DEBUG_EVENTS=1`, local/test only):**

| Type | Description |
|------|------|
| `llm.request` | Upstream request payload (excludes secrets) |
| `llm.response.chunk` | Upstream raw streaming chunk |

### 4.3 Association Constraints

- `tool.call` / `tool.result` are associated via `tool_call_id`.
- `seq` strictly increases within the same run.
- Events are first stored in the database (`run_events` table), then pushed via PG `LISTEN/NOTIFY`.

## 5. Error Model

Unified error response:

```json
{
  "code": "auth.invalid_credentials",
  "message": "...",
  "details": {},
  "trace_id": "..."
}
```

The HTTP header also returns `X-Trace-Id`.

Error Classifications:

| Prefix | Description |
|------|------|
| `auth.*` | Authentication/Permissions |
| `validation.*` | Schema validation |
| `policy.*` | Policy interception |
| `budget.*` | Budget/Quota |
| `provider.*` | Model provider errors |
| `mcp.*` | MCP protocol errors (timeout/disconnected/rpc_error/protocol_error/tool_error) |
| `internal.*` | Internal errors |

The `trace_id` is generated by the server. Trusted upstreams (Gateway) can pass through `X-Trace-Id` (`ARKLOOP_TRUST_INCOMING_TRACE_ID=1`); those from untrusted clients are ignored.

## 6. Middleware Stack

Request processing order:

1. **TraceMiddleware** -- Generates/validates trace_id, parses client IP.
2. **RecoverMiddleware** -- Panic recovery and error logging.
3. **Auth Middleware** -- Token validation, role checking.
4. **Entitlement Middleware** -- Quota/feature permission checking.
5. **Audit Logging** -- Writes to the `audit_logs` table.

## 7. Configuration (API-related env)

| Variable | Description |
|------|------|
| `ARKLOOP_API_GO_ADDR` | Listening address (default `127.0.0.1:8001`) |
| `ARKLOOP_DATABASE_URL` | PostgreSQL connection |
| `ARKLOOP_DATABASE_DIRECT_URL` | Direct connection (SSE LISTEN/NOTIFY, bypassing PgBouncer) |
| `ARKLOOP_REDIS_URL` | Redis (rate limiting, run concurrency control) |
| `ARKLOOP_AUTH_JWT_SECRET` | JWT signing secret |
| `ARKLOOP_ENCRYPTION_KEY` | AES-256-GCM key (64 hex) |
| `ARKLOOP_S3_*` | MinIO/S3 object storage |
| `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` | Initial admin username |
| `ARKLOOP_TRUST_INCOMING_TRACE_ID` | Trust upstream trace_id |
| `ARKLOOP_TRUST_X_FORWARDED_FOR` | Trust X-Forwarded-For |
| `ARKLOOP_MAX_CONCURRENT_RUNS_PER_ORG` | Max concurrent runs per organization (default 10) |
| `ARKLOOP_SSE_HEARTBEAT_SECONDS` | SSE heartbeat interval (default 15) |
| `ARKLOOP_SSE_BATCH_LIMIT` | SSE batch limit (default 500) |
| `ARKLOOP_RUN_TIMEOUT_MINUTES` | Run timeout (default 5) |
| `ARKLOOP_RUN_EVENTS_RETENTION_MONTHS` | Event partition retention in months (default 3) |
| `ARKLOOP_APP_BASE_URL` | Frontend URL |
| `ARKLOOP_TURNSTILE_*` | Cloudflare Captcha |
| `ARKLOOP_EMAIL_FROM` | Sender email address |

## 8. SSE Authentication

Uses **Fetch streaming + `Authorization: Bearer`**:
- SSE and standard APIs use the same authentication mechanism.
- Consistent implementation for Web and CLI.
- Frontend uses `after_seq` cursor for disconnection reconnection.
- `run_events` does not contain sensitive plaintext (model keys, system prompt source).
