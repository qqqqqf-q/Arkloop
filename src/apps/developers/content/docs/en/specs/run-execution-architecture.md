---
title: "Run Execution Architecture (API / Worker / Supporting Services)"
---
This document describes Arkloop's Run execution topology and service boundaries. Core design: complete separation of the control plane and execution plane.

- **API**: Authentication, resource orchestration, audit logging, SSE playback, enqueueing jobs
- **Worker**: Executing Agent Loop, tool calls, event writing
- **Gateway**: Reverse proxy, rate limiting, access logs
- **Sandbox**: Code execution in Firecracker microVMs
- `run_events` as the single source of truth

## 1. Current Topology

```
Client (Web/CLI) --HTTP+SSE--> API (Go, Control Plane)
                                 |
                                 v
                        PostgreSQL (runs/run_events/messages/jobs/audit_logs)
                                 ^
                                 |
                        Worker (Go, Execution Plane) --lease jobs--> PostgreSQL
                                 |
                        +--------+--------+--------+
                        |        |        |
                     LLM API   MCP    Sandbox
                  (OpenAI/     Server  (Firecracker)
                   Anthropic)
```

Key constraints:
- API does not execute Agent Loop or trigger any tool executors
- Worker is the sole source of truth for the execution plane
- API scalability comes from being a lightweight control plane (DB CRUD + SSE playback)
- Redis is used for rate limiting and organization-level concurrent run control

## 2. Core Invariants

- `run_events`: The single source of truth (written by Worker, read and played back via SSE by API)
- `jobs.payload_json`: Cross-service protocol (written by API, read by Worker), must be versioned
- `seq`: Monotonically increasing within a run; playback uses `after_seq` for resumption

## 3. Minimum Closed-Loop Flow

1) Client creates run: `POST /v1/threads/{id}/runs`

2) API (within the same transaction):
   - Writes `runs` row
   - Writes the first event `run.started`
   - Inserts `jobs` (`run.execute`)

3) Worker consumes `jobs`:
   - Leases job (PostgreSQL Advisory Lock)
   - Executes Pipeline (Middleware chain -> Agent Loop)
   - Appends `run_events`
   - Writes `messages` (assistant merged results)
   - Notifies API via PG `NOTIFY`

4) Client replays via SSE: `GET /v1/runs/{id}`
   - Uses `after_seq` as the sole cursor
   - When `follow=true`, API sends heartbeats (15s) to prevent proxy disconnection

## 4. Worker Pipeline Middleware

Worker uses a middleware chain pattern to process runs, executing in order:

| No. | Middleware | Responsibility |
|------|--------|------|
| 1 | `mw_cancel_guard` | Handling cancellation signals (fail early) |
| 2 | `mw_input_loader` | Loading run inputs and thread messages |
| 3 | `mw_entitlement` | Checking quota/feature permissions (e.g., runs/tokens quota) |
| 4 | `mw_mcp_discovery` | Discovering MCP tools (account-level + cache) |
| 5 | `mw_tool_provider` | Injecting Tool Providers (account overrides platform) and binding executors |
| 6 | `mw_persona_resolution` | Resolving persona_id, system prompt, and persona config |
| 7 | `mw_channel_context` | Injecting channel context (Telegram group/user merge) |
| 8 | `mw_heartbeat_schedule` | Scheduling heartbeat runs for active Telegram groups |
| 9 | `mw_channel_telegram_group_user_merge` | Merging Telegram group and user contexts |
| 10 | `mw_channel_group_context_trim` | Trimming channel group context |
| 11 | `mw_channel_telegram_tools` | Injecting Telegram-specific tools |
| 12 | `mw_subagent_context` | Injecting sub-agent control context |
| 13 | `mw_skill_context` | Injecting enabled skills into runtime |
| 14 | `mw_memory` | Injecting long-term memory (optional) |
| 15 | `mw_trust_source` | Trust source configuration |
| 16 | `mw_injection_scan` | Scanning for prompt injection attacks |
| 17 | `mw_routing` | LLM Provider routing decisions, building Gateway |
| 18 | `mw_title_summarizer` | Generating conversation titles (optional) |
| 19 | `mw_context_compact` | Context compaction for long conversations |
| 20 | `mw_heartbeat_prepare` | Preparing heartbeat run context |
| 21 | `mw_tool_description_override` | Overriding tool descriptions from DB |
| 22 | `mw_platform` | Injecting platform management tools |
| 23 | `mw_tool_build` | Building ToolSpecs and tool dispatcher (filtering by allowlist) |
| 24 | `mw_result_summarizer` | Summarizing run results |
| 25 | `mw_channel_delivery` | Delivering run events to channel |
| 26 | `handler_agent_loop` | Main Agent Loop (LLM calls + tool execution) |

### 4.1 Middleware Test Coverage

Core middlewares have independent unit tests (`go test -race`):

- `mw_input_loader`: nil pool panic detection, message quota fallback, chain execution verification
- `mw_entitlement`: nil resolver passthrough, credits exhaustion panic path, releaseSlot callback
- `mw_persona_resolution`: system prompt layering, parameter clamping, tool allowlist/denylist intersection
- `mw_routing`: stub gateway, static route fallback, provider kind dispatch, API key validation
- `mw_memory`: nil provider/nil userID no-op, injection/commit/error degradation

Security related: `web_fetch/url_policy_test.go` covers the full SSRF interception path (scheme, hostname, localhost, private IP, IPv6).

## 5. Executor Types

Worker supports multiple executors via Executor Registry:

| Executor | Description |
|--------|------|
| `native_v1` | Standard Agent (tool call loop) |
| `agent.simple` | Basic prompt-only execution |
| `task.classify_route` | Task classification routing (deciding Pro/Ultra) |
| `agent.lua` | Lua script Agent (supports `context.emit()` for custom events) |
| `agent.interactive` | Human-in-the-loop mode |
| `noop` | No operation (for testing) |

## 6. Personas System

Personas are loaded from the `src/personas/` directory and the database. Each Persona includes:
- `persona.yaml` -- Metadata and configuration
- `prompt.md` -- System Prompt
- `agent.lua` -- Lua script (optional)

Current built-in Personas:

| Persona | Executor | Description |
|-------|--------|------|
| `auto` | `task.classify_route` | Classifies task complexity and routes to Pro/Ultra |
| `lite` | `agent.simple` | Lightweight fast response |
| `pro` | `agent.simple` | General capability + tools |
| `search` | `agent.simple` | Search optimized (includes `web_search` tool) |
| `ultra` | `agent.simple` | Maximum reasoning depth |

Persona configuration fields: `id`, `executor_type`, `executor_config`, `tool_allowlist`, `tool_denylist`, `budgets` (reasoning_iterations, tool_continuation_budget, max_output_tokens, temperature, top_p, per_tool_soft_limits).

## 7. Tool Execution

### 7.1 Built-in Tools

| Tool | Description |
|------|------|
| `web_search` | Search (execution backend can be overridden by Console's Tool Providers config) |
| `web_fetch` | Fetch (execution backend can be overridden by Console's Tool Providers config) |
| `sandbox` | Code execution (Firecracker microVM) |
| `spawn_agent` | Spawning child Agent runs |
| `summarize_thread` | Conversation summary |
| `echo` | Echo for testing |
| `noop` | No operation |

### 7.2 Tool Security

- **Allowlist**: `ARKLOOP_TOOL_ALLOWLIST` (deprecated; logged for compatibility only and no longer gates runtime tools)
- **Denylist**: Persona-level `tool_denylist`
- LLM only sees tools within the allowlist
- Each tool execution has a timeout control (`tool_timeout_ms`)

Note: `web_search` / `web_fetch` are Tool Group names (LLM only sees the group). At runtime, Worker resolves these to specific Providers (e.g., `web_search.tavily`, `web_fetch.jina`). Resolution priority is:

1) Provider activated at org scope (DB `tool_provider_configs.scope='org'`)
2) Provider activated at platform scope (DB `tool_provider_configs.scope='platform'`)
3) Legacy group executor (Config Resolver's `web_search.*` / `web_fetch.*`, supports env override)
4) Otherwise returns `tool.not_configured`

Note: Activation/credentials for Tool Providers are managed via the Console's Tool Providers configuration page (`/v1/tool-providers`). platform scope is for global defaults; org scope is for tenant overrides.

### 7.3 MCP Tools

- Supports both Stdio (child process) and HTTP transports
- Each Org configures MCP servers independently (`mcp_configs` table)
- Discovery results are cached (TTL: `ARKLOOP_MCP_CACHE_TTL_SECONDS`, default 60s)
- Error classification: `mcp.timeout`, `mcp.disconnected`, `mcp.rpc_error`, `mcp.protocol_error`, `mcp.tool_error`

## 8. Provider Routing

Routing configuration is loaded from provider accounts and model routes in the database:
- Checks `route_id` in the run request
- Validates route existence, credential accessibility, and whether BYOK is enabled
- Output: `SelectedProviderRoute` or `ProviderRouteDenied` (`policy.route_not_found`, `policy.byok_disabled`)
- When no DB routing config is available, Worker can fall back to environment variable static routes (dev/self-hosted scenarios)

Supported LLM Providers:
- OpenAI (and compatible APIs)
- Anthropic
- Stub (for testing)

LLM Retry Strategy: Auto retry for 429/502/503 with exponential backoff (default 3 attempts, 1s base delay).

## 9. Supporting Services

### 9.1 Gateway (`src/services/gateway/`)

HTTP reverse proxy located in front of the API:
- Rate limiting
- Access logs
- Request forwarding

### 9.2 Sandbox (`src/services/sandbox/`)

Firecracker microVM code execution:
- Called by Worker via `ARKLOOP_SANDBOX_BASE_URL`
- Isolated execution of user code

### 9.3 OpenViking (External Service)

User memory system:
- Called by Worker via `ARKLOOP_OPENVIKING_BASE_URL` + `ARKLOOP_OPENVIKING_ROOT_API_KEY`
- Loading/saving user memory snapshots
- Data stored in `user_memory_snapshots` table

## 10. Task Queue

Task queue implemented using PostgreSQL tables:

| Task Type | Description |
|----------|------|
| `run.execute` | Executing Agent Loop |
| `webhook.deliver` | Delivering Webhooks |
| `email.send` | Sending Emails |

Queue parameters:
- Concurrency: `ARKLOOP_WORKER_CONCURRENCY` (default 4)
- Polling interval: `ARKLOOP_WORKER_POLL_SECONDS` (default 0.25s)
- Lease duration: `ARKLOOP_WORKER_LEASE_SECONDS` (default 30s)
- Heartbeat interval: `ARKLOOP_WORKER_HEARTBEAT_SECONDS` (default 10s)

Workers register their capabilities and versions in the `worker_registrations` table upon startup.

## 11. Configuration (Worker Related Env)

| Variable | Description |
|------|------|
| `ARKLOOP_DATABASE_URL` | PostgreSQL connection |
| `ARKLOOP_WORKER_CONCURRENCY` | Concurrency (default 4) |
| `ARKLOOP_WORKER_POLL_SECONDS` | Polling interval (default 0.25) |
| `ARKLOOP_WORKER_LEASE_SECONDS` | Lease duration (default 30) |
| `ARKLOOP_WORKER_HEARTBEAT_SECONDS` | Heartbeat interval (default 10) |
| `ARKLOOP_WORKER_QUEUE_JOB_TYPES` | Task types to consume |
| `ARKLOOP_WORKER_CAPABILITIES` | Worker capability tags |
| `ARKLOOP_WORKER_VERSION` | Worker version |
| `ARKLOOP_TOOL_ALLOWLIST` | Deprecated compatibility flag; no longer gates runtime tools |
| `ARKLOOP_LLM_RETRY_MAX_ATTEMPTS` | LLM retry attempts (default 3) |
| `ARKLOOP_LLM_RETRY_BASE_DELAY_MS` | Retry base delay (default 1000) |
| `ARKLOOP_MCP_CACHE_TTL_SECONDS` | MCP discovery cache TTL (default 60) |
| `ARKLOOP_TOOL_PROVIDER_CACHE_TTL_SECONDS` | Tool Provider cache TTL (default 60) |
| `ARKLOOP_LLM_DEBUG_EVENTS` | Debug events toggle |
| `ARKLOOP_SANDBOX_BASE_URL` | Sandbox service address |
| `ARKLOOP_OPENVIKING_BASE_URL` | Memory system address |
| `ARKLOOP_OPENVIKING_ROOT_API_KEY` | Memory system key |
| `ARKLOOP_ENCRYPTION_KEY` | Credential decryption key |

## 12. Common Issues (Troubleshooting Perspective)

- **"run stuck in running"**: Check if `jobs` are leased by a Worker, if `run_events` are being written, and if Worker heartbeats are normal.
- **"SSE occasionally hangs"**: Check if proxies are buffering (API should set `Cache-Control: no-cache`, `X-Accel-Buffering: no`) and verify heartbeats.
- **"Events missing/out of order"**: `seq` must strictly increase within the same run; playback must use `after_seq` for resumption.
- **"Tools not responding"**: Check supporting service availability and runtime registration state; `ARKLOOP_TOOL_ALLOWLIST` no longer gates tools.
- **"MCP tool timeout"**: Check `mcp_configs` configuration, MCP server process status, and cache TTL.
