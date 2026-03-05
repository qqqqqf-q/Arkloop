# Database Architecture and Data Models

This document describes Arkloop's database boundaries, core tables, and architectural constraints for permissions, auditing, and billing. The production environment uses PostgreSQL as the sole target backend.

Migration Tool: Goose (embedded in `src/services/api/internal/migrate/migrations/`, with 80 migration files).

## 1. Terminology

`org` is the **tenant boundary**:
- Data isolation boundary (permissions, exports, deletion, retention policies).
- Auditing boundary (log attribution and accountability scope).
- Billing and quota boundary (budgets, multipliers, usage reports).

`platform` is the **global scope of the deployment instance**:
- Platform-level default configurations and platform-level credentials (ensures a new org can run without configuration).
- Managed by `platform_admin`, not belonging to any specific org.
- Org-level configurations are for overrides only and should not act as "global defaults."

## 2. Top-level Structure: `org / team / project`

### 2.1 `orgs` (Tenants/Companies)

| Column | Description |
|----|------|
| `id` | PK |
| `slug` | URL-friendly identifier |
| `name` | Display name |
| `created_at` | Creation time |

### 2.2 `users` (User Entities)

| Column | Description |
|----|------|
| `id` | PK |
| `username` | Username |
| `created_at` | Creation time |

### 2.3 `org_memberships` (Organization Memberships)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `user_id` | FK -> users |
| `role` | Role (owner / member) |

### 2.4 `teams` (Groups within an Organization)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `name` | Name |

### 2.5 `projects` (Projects/Collaboration Domains)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `team_id` | FK -> teams (optional) |
| `name` | Name |
| `description` | Description |
| `visibility` | Visibility |
| `deleted_at` | Soft delete flag |

## 3. Threads and Messages

### 3.1 `threads` (Conversation Containers)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `created_by_user_id` | FK -> users |
| `title` | Title |
| `project_id` | FK -> projects (optional) |
| `agent_config_id` | FK -> agent_configs (optional) |
| `private` | Private flag |
| `deleted_at` | Soft delete |
| `created_at` | Creation time |

### 3.2 `messages` (Messages)

| Column | Description |
|----|------|
| `id` | PK |
| `thread_id` | FK -> threads |
| `org_id` | FK -> orgs |
| `role` | user / assistant / system |
| `content` | Text content |
| `content_json` | JSONB structured content |
| `hidden` | Hidden flag |
| `created_at` | Creation time |

## 4. Runs and Events

### 4.1 `runs` (Execution Instances)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `thread_id` | FK -> threads |
| `created_by_user_id` | FK -> users |
| `status` | State machine |
| `parent_run_id` | FK -> runs (sub-run) |
| `created_at` | Creation time |
| `updated_at` | Update time |

### 4.2 `run_events` (Event Stream -- Single Source of Truth)

**Partitioned by month** (`created_at`), with automatic partition lifecycle management (`ARKLOOP_RUN_EVENTS_RETENTION_MONTHS`).

| Column | Description |
|----|------|
| `event_id` | PK |
| `run_id` | FK -> runs |
| `seq` | Monotonically increasing sequence within the run |
| `ts` | Server-side timestamp |
| `type` | Event type |
| `data_json` | JSONB payload |
| `tool_name` | Column index |
| `error_class` | Column index |
| `created_at` | Partition key |

Key Constraints:
- `seq` strictly increases within the same run.
- Written by the Worker, read and replayed as SSE by the API.
- Supports `after_seq` cursor for resuming after disconnection.

## 5. LLM Credentials and Routes

### 5.1 `llm_credentials` (LLM Provider Credentials)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs (currently an org-level resource) |
| `provider` | Provider identifier |
| `name` | Display name |
| `secret_id` | FK -> secrets (stored encrypted) |
| `key_prefix` | Key prefix (for identification) |
| `base_url` | Custom base URL |
| `advanced_json` | JSONB advanced configuration |

### 5.2 `llm_routes` (Model Routing Rules)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `credential_id` | FK -> llm_credentials |
| `model` | Model identifier |
| `priority` | Priority |
| `is_default` | Default route flag |
| `when_json` | JSONB conditional rules |
| `multiplier` | Rate multiplier |
| `cache_pricing_json` | Cache pricing |

### 5.3 `secrets` (Generic Encrypted Storage)

Encrypted with AES-256-GCM, using the key provided by `ARKLOOP_ENCRYPTION_KEY`.

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs (required for org scope; NULL for platform scope) |
| `scope` | `org` / `platform` |
| `name` | Logical key (unique within the same scope) |
| `encrypted_value` | Encrypted value (base64) |
| `key_version` | Encryption version |
| `rotated_at` | Rotation time (optional) |
| `created_at` | Creation time |
| `updated_at` | Update time |

Constraints:
- `scope='org'`: `(org_id, name)` must be unique.
- `scope='platform'`: `name` must be globally unique.

`secrets` usage:
- API Keys for LLM / ASR credentials.
- API Keys for Tool Providers.

Currently: Configuration items in the Config Registry marked as `Sensitive=true` are masked when returned by the API; values are written to `platform_settings/org_settings` unencrypted.

### 5.4 `platform_settings` / `org_settings` (Unified Configuration: Config Resolver)

Used for Track A Config Resolver (key-value configuration), supporting platform defaults and org overrides.

#### `platform_settings`

| Column | Description |
|----|------|
| `key` | PK |
| `value` | Configuration value (non-sensitive) |
| `updated_at` | Update time |

#### `org_settings`

| Column | Description |
|----|------|
| `org_id` | FK -> orgs |
| `key` | Configuration key |
| `value` | Configuration value (non-sensitive) |
| `updated_at` | Update time |

Resolver Priority Chain (high to low):
1) ENV override (forced deployment layer override)
2) `org_settings`
3) `platform_settings`
4) Registry default value

### 5.5 `tool_provider_configs` (Tool Backend Activation and Credential Association)

Used for backend selection, credentials, and base_url configuration for Tool Groups such as `web_search` and `web_fetch`.

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs (required for org scope; NULL for platform scope) |
| `scope` | `org` / `platform` |
| `group_name` | Tool Group name (tool name as seen by LLM, e.g., `web_search`) |
| `provider_name` | Provider name (internal tool name, e.g., `web_search.tavily`) |
| `is_active` | Activation status (at most one active per scope + group) |
| `secret_id` | FK -> secrets (API Key, stored encrypted) |
| `key_prefix` | Key prefix (for Console display) |
| `base_url` | Custom endpoint (SearXNG / self-hosted Firecrawl, etc.) |
| `config_json` | Non-sensitive parameters (JSONB) |
| `created_at` | Creation time |
| `updated_at` | Update time |

Resolution Chain:
- Org scope active provider prioritized.
- Falls back to platform scope active provider if no org configuration exists.

## 6. Personas and Agent Configurations

### 6.1 `personas` (Persona Definitions)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `persona_key` | Persona identifier |
| `version` | Version |
| `display_name` | Display name |
| `description` | Description |
| `prompt_md` | System prompt |
| `tool_allowlist` | Allowed tools list |
| `tool_denylist` | Forbidden tools list |
| `preferred_credential` | Preferred credential |
| `agent_config_name` | Associated agent configuration |

### 6.2 `agent_configs` (Agent Configurations)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `name` | Configuration name |
| `system_prompt_override` | System prompt override |
| `model` | Model identifier |
| `temperature` | Temperature |
| `max_output_tokens` | Maximum output tokens |
| `tool_policy` | Tool policy |
| `tool_allowlist` | Tool whitelist |
| `cache_control_json` | Cache control |
| `reasoning_mode` | Reasoning mode |
| `scope` | Scope |

## 7. Billing and Quotas

### 7.1 `plans` (Subscription Plans)

| Column | Description |
|----|------|
| `id` | PK |
| `name` | Plan identifier |
| `display_name` | Display name |

### 7.2 `subscriptions` (Subscription Relations)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `plan_id` | FK -> plans |
| `status` | Status |
| `current_period_start` | Current period start |
| `current_period_end` | Current period end |
| `cancelled_at` | Cancellation time |

### 7.3 `plan_entitlements` (Plan Feature Quotas)

| Column | Description |
|----|------|
| `id` | PK |
| `plan_id` | FK -> plans |
| `key` | Feature key |
| `value` | Quota value |
| `value_type` | Value type |

### 7.4 `org_entitlement_overrides` (Organization-level Overrides)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `key` | Feature key |
| `value` | Override value |
| `reason` | Reason |
| `expires_at` | Expiration time |

### 7.5 `credits` / `credit_transactions` (Credits System)

| Table | Key Columns |
|----|--------|
| `credits` | org_id, amount, balance |
| `credit_transactions` | credits_id, amount, type |

### 7.6 `usage_records` (Usage Records)

Cached columns: `input_tokens`, `output_tokens`, `cache_hit_rate`.

## 8. Social and Sharing

| Table | Description |
|----|------|
| `thread_stars` | Stars (thread_id + user_id) |
| `thread_shares` | Shares (shared_by_user_id, recipient_user_id) |
| `thread_reports` | Reports (reason, status) |

## 9. Infrastructure

### 9.1 `jobs` (Background Task Queue)

Task queue implemented using a PostgreSQL table and Advisory Locks.

| Column | Description |
|----|------|
| `id` | PK |
| `job_type` | Type (`run.execute` / `webhook.deliver` / `email.send`) |
| `payload_json` | JSONB payload (cross-language protocol, must be versioned) |
| `status` | Status |
| `available_at` | Available time |
| `leased_until` | Lease expiration |
| `attempts` | Retry attempts |
| `worker_tags` | Worker capability tags |

### 9.2 `worker_registrations` (Worker Registrations)

| Column | Description |
|----|------|
| `id` | PK |
| `name` | Worker name |
| `capabilities_json` | Capabilities set |
| `heartbeat_at` | Heartbeat time |

### 9.3 `webhook_endpoints` (Webhooks)

| Column | Description |
|----|------|
| `id` | PK |
| `url` | Callback URL |
| `events` | Array of subscribed event types |
| `active` | Active status |

### 9.4 `api_keys` (API Keys)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `key_prefix` | Key prefix |
| `last_used_at` | Last used time |

## 10. Authentication and Security

| Table | Description |
|----|------|
| `user_credentials` | Login credentials (login, password_hash) |
| `refresh_tokens` | JWT refresh tokens (user_id, token, revoked_at) |
| `email_verification_tokens` | Email verification |
| `email_otp_tokens` | OTP (email, code, expires_at) |
| `rbac_roles` | Role definitions (permissions_json) |

## 11. Notifications and Auditing

| Table | Description |
|----|------|
| `notifications` | User notifications (type, title, body, read_at) |
| `notification_broadcasts` | Platform broadcasts (soft delete) |
| `audit_logs` | Audit logs (user_id, action, resource_type, ip_address, user_agent) |

## 12. MCP and External Integrations

### 12.1 `mcp_configs` (MCP Server Configurations)

| Column | Description |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `name` | Server name |
| `url` | Connection URL |
| `env_json` | Environment variables |
| `tools_json` | Tool definitions |

### 12.2 `asr_credentials` (Speech-to-Text Credentials)

Structure similar to `llm_credentials`, managed independently.

## 13. Miscellaneous

| Table | Description |
|----|------|
| `user_memory_snapshots` | User memory snapshots (org_id, data_json, hits_json), interfaces with OpenViking |
| `platform_settings` | Global platform configuration (key-value JSONB) |
| `feature_flags` | Feature flags |
| `redemption_codes` | Redemption codes (value, usage_count, expires_at) |
| `invite_codes` | Invite codes |

## 14. Architecture Decision Records

- **Storage Engine**: PostgreSQL (sole production backend).
- **Encryption**: AES-256-GCM (`ARKLOOP_ENCRYPTION_KEY`), used for `llm_credentials`, `asr_credentials`, and `secrets`.
- **Partitioning**: `run_events` partitioned by month (`created_at`), with automatic cleanup of expired partitions.
- **Soft Deletion**: `threads`, `notification_broadcasts`, and `projects` use `deleted_at`.
- **UUID**: Primary keys use UUID (`pgcrypto` extension).
- **Task Queue**: PostgreSQL table + Advisory Lock (no dependency on external MQ).
- **Real-time Push**: PostgreSQL `LISTEN/NOTIFY` -> SSE.
- **Credential Scopes**: `llm_credentials` supports both platform-level (`org_id` is NULL) and org-level scopes.
