# Logging and Observability Strategy

This document describes Arkloop's logging system architecture, covering the roles of application logs, audit logs, and Run Events.

## 1. Boundaries of the Three Record Types

| Category | Purpose | Storage |
|------|------|------|
| **Run Events** | Business event stream (SSE + storage + playback) | `run_events` table (partitioned by month) |
| **Application Logs** | Operational troubleshooting (service health, exceptions, latency) | stdout JSON |
| **Audit Logs** | Security auditing (management actions, access, permission changes) | `audit_logs` table |

This strategy focuses on Application Logs and requires coordination with Run Events fields.

## 2. Code Attribution

| Service | Path |
|------|------|
| API | `src/services/api/internal/observability/` + `src/services/api/internal/http/` |
| Worker | `src/services/worker/internal/app/` (logger configuration + trace_id propagation) |

Principles:
- Core logic expresses processes through Run Events, not direct dependency on logging.
- Logging is configured by the API/Worker composition root; business modules only receive the pre-configured logger / context accessor.

## 3. Structured JSON (stdout)

All services output single-line JSON to stdout:
- Timestamps are unified as ISO8601 (UTC).
- `exception`/`stack` are serialized as fields.
- Compatible with Loki/ELK/Datadog collection.

## 4. End-to-End trace_id Propagation

### 4.1 Generation and Propagation

- The HTTP entry point (TraceMiddleware) generates the `trace_id`.
- Written to: log fields, API error responses, HTTP header `X-Trace-Id`, and Run Events.
- Propagated to the Worker via `jobs.payload_json`, where the Worker restores the context.

### 4.2 Trust Policy

- `trace_id` from standard clients is untrusted.
- Trusted upstreams (Gateway): `ARKLOOP_TRUST_INCOMING_TRACE_ID=1`.
- Client IP: `ARKLOOP_TRUST_X_FORWARDED_FOR=1` (enabled only in reverse proxy scenarios).

### 4.3 Correlation ID Distinction

| ID | Description |
|----|------|
| `trace_id` | End-to-end tracing |
| `request_id` | Single HTTP request |
| `run_id` | Agent Loop execution instance |

## 5. Automatic Context Injection

Logs automatically populate fields through context binding, avoiding line-by-line manual entry.

Required context fields (priority high to low):
- `trace_id`, `request_id`
- `org_id`, `user_id`
- `project_id`, `thread_id`, `run_id`
- `tool_call_id`, `event_id`
- `component` (`api` / `worker` / `gateway`)

## 6. Data Masking Policy

**Never Log:**
- `Authorization`, `Cookie`, model provider keys, system prompt source.

**Tool Parameters/Outputs:**
- Application logs only record `tool_name`, duration, and error classification.
- Plaintext parameters enter Run Events (masked/classified by policy).

**User Inputs/Model Outputs:**
- Application logs only record length/summary.
- Playback and auditing rely on Run Events.

## 7. Log Field Schema

Field naming: snake_case + lowercase (`trace_id`, not `traceId`).

| Category | Field |
|------|------|
| Common | `ts`, `level`, `logger`, `msg`, `component`, `env`, `version` |
| Correlation | `trace_id`, `request_id`, `org_id`, `user_id`, `project_id`, `thread_id`, `run_id` |
| Execution | `duration_ms`, `attempt`, `timeout_ms` |
| Tool | `tool_name`, `tool_call_id`, `risk_level` |
| Cost | `provider`, `model`, `input_tokens`, `output_tokens`, `cost_usd` |
| Error | `error_class`, `error_code`, `exception`, `stack` |

## 8. Error Classification

Aligned with the API error model:

| Classification | Description |
|------|------|
| `auth.*` | Authentication/Permissions |
| `validation.*` | Schema validation |
| `policy.*` | Policy interception |
| `budget.*` | Budget/Quota |
| `provider.*` | Model provider errors |
| `mcp.*` | MCP protocol errors |
| `internal.*` | Internal errors |

Additional fields: `retryable` (retry flag), `duration_ms`, `cost_usd`.

## 9. Roles of Run Events vs. Application Logs

| Scenario | Write Target |
|------|----------|
| Who called which tool, parameters/results, policy interception, budget changes | Run Events |
| Dependency exceptions, timeouts, database errors, Worker crashes, upstream instability | Application Logs |

When the same fact is needed by both:
- Run Events retain business semantics.
- Application logs only retain correlation fields + duration, avoiding duplicate sensitive plaintext.

## 10. Audit Logs

The `audit_logs` table records all management operations:

| Field | Description |
|------|------|
| `user_id` | Actor |
| `action` | Action type |
| `resource_type` | Resource type |
| `ip_address` | Source IP |
| `user_agent` | Client identifier |

Any unauthorized viewing/export/policy changes must be recorded in audit logs.

## 11. OpenTelemetry Evolution Path

Current: `trace_id` is used for end-to-end tracing.

Future: OTel will be introduced as an enhancement, requiring alignment with log fields (`trace_id` / `span_id`). Its introduction will not break the existing log schema.
