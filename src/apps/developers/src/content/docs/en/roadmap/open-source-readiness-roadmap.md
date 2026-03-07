---
---

# Arkloop Open Source Readiness Roadmap

This document serves as the unified roadmap for the open-source release. It integrates **unfinished work** from the three existing roadmaps (development-roadmap, architecture-refactor-roadmap, and agent-system-roadmap) while introducing four new dimensions: architecture governance, code sharing, plugin systems, and infrastructure development.

Related Documents (Historical Reference):
- `docs/roadmap/development-roadmap.md` -- Archived, no new content
- `docs/roadmap/architecture-refactor-roadmap.md` -- Archived, no new content
- `docs/roadmap/agent-system-roadmap.md` -- Archived, no new content
- `docs/architecture/architecture-design-v2.md` -- Reference for the target architecture
- `docs/architecture/architecture-problems.md` -- Architecture audit report

---

## 0. Current System Baseline

### 0.1 Delivered Capabilities

**Infrastructure Layer**: PostgreSQL + PgBouncer + Redis + MinIO + Gateway (Go reverse proxy), fully orchestrated via `compose.yaml`.

**API Service**: JWT double token authentication, RBAC, Teams/Projects, Org invitations, API Key management, IP filtering, Rate Limiting, SSE pushing, monthly partitioning for `run_events`, Feature Flags, Invitation/Redemption/Credit systems, Webhooks, and Entitlements/Plans.

**Worker Execution Engine**: Pipeline middleware chain, Executor Registry (SimpleExecutor / InteractiveExecutor / ClassifyRouteExecutor / LuaExecutor), Personas (YAML + DB dual source), MCP connection pool, Provider routing (when condition matching + default fallback), Human-in-the-loop (WaitForInput + input_requested), Sub-agent Spawning (parent_run_id + spawn_agent tool), Memory System (OpenViking adapter + memory_search/read/write/forget tools), and Cost Budget tracking (RunContext.ToolBudget reservation, enforcement pending on execution side).

**Independent Services**: Sandbox (Firecracker microVM + Warm Pool + Snapshot + MinIO persistence), Browser Service (Playwright + Session Manager + BrowserPool), and OpenViking (Python HTTP memory service).

**Frontend**: Web App (React + Vite + TypeScript), Console (React admin dashboard with eight modules: operations, configuration, integration, security, organization, billing, and platform), and CLI (reference client).

### 0.2 Core Issues

The following are structural deficiencies in the current system regarding open-source readiness:

**P1 -- No Unified Abstraction for Configuration Management**

Three configuration reading paths run in parallel (direct ENV reading, `platform_settings` DB queries, and file reading) without a unified `config.Resolve(key, scope)` interface. Each tool builds its own `config_db.go` (three nearly identical sets of code for email, web_search, and web_fetch). Adding a configuration point requires copy-pasting. Hardcoded magic numbers are scattered across 20+ files, inaccessible to the Console.

**P2 -- Inconsistent Scope Resolution**

Agent Config follows a four-level resolution (thread -> project -> org -> platform); ASR Credentials follow two levels (org -> platform); web_search/email only read `platform_settings` without distinguishing orgs; browser/sandbox constructor injection lacks dynamic resolution. Scope resolution has four different implementations within the same system, leaving no clear reference for new modules.

**P3 -- Missing Tool Provider Management**

Hardcoded backend switching logic for web_search (Tavily/SearXNG) and web_fetch (Jina/Firecrawl/Basic). No per-org Provider activation, no Console management entry, and no `AgentToolSpec.LlmName` dual-name mechanism. AS-11 was designed but remains unimplemented.

**P4 -- Completely Fragmented Frontend Code**

Web and Console now share basic building blocks via `@arkloop/shared` (e.g., `apiFetch`, token logic), but page/component/locale implementations still diverge heavily, increasing maintenance cost.

**P5 -- Opaque System Limits**

Limits like `threadMessageLimit` (200), `maxInputContentBytes` (32KB), `defaultReasoningIterations` (10), `maxParallelTasks` (32), and `entitlement` defaults (999,999 runs) are hardcoded without centralized registration, documentation exposure, or Console adjustability. Users and developers only discover these limits upon hitting them.

**P6 -- Lack of Quality Assurance Infrastructure**

No stress testing baseline, no CI pipelines, and no automated quality gates. Code merging relies entirely on manual judgment.

**P7 -- Unclear Open Source Compliance and Copyright Boundaries**

The repository lacks clear open-source licenses (LICENSE/NOTICE) and a list of third-party dependency licenses. The current directory structure mixes commercial/legal documents (`docs/`) with internal engineering documents (`docs/`), making open-source boundaries fuzzy and risking accidental disclosure or irreversible open-sourcing.

**P8 -- Repository Hygiene and Secret Leakage Risk**

While `.env` is ignored in `.gitignore`, no systematic secret scanning or historical audit process has been established. If keys, tokens, or internal addresses remain in the git history before open-sourcing, it will cause irreversible leakage risks.

**P9 -- Missing Open Source Governance and Contribution Entry**

Lack of "community standard files" like `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, and Issue/PR templates. External contributors lack a predictable collaboration loop; vulnerability reporting paths are unclear, making security response uncontrollable.

**P10 -- Opaque Deployment Profiles and Platform Dependencies**

The `compose.yaml` defaults to including Sandbox (Firecracker microVM) requiring privileges and KVM, which is unavailable or provides a poor experience on macOS/Windows. The absence of a "minimal runnable set" compose profile and feature matrix leads to a high failure rate for first-time open-source users.

**P11 -- Supply Chain and Dependency Risks Not Integrated into Gates**

CI only covers compilation and testing, lacking dependency vulnerability scanning, license scanning, image scanning, SBOM generation, and release signing. Maintenance costs and security risks will be magnified after open-sourcing.

**P12 -- Missing CORS Middleware in Gateway/API**

No CORS handling logic exists in any Gateway or API Go code (no occurrences of `Access-Control-Allow-Origin`). When the frontend (Web/Console) and backend are deployed separately, browsers will reject cross-origin requests. This is not just a "contributor experience" issue; it prevents the project from running out of the box. A configurable CORS middleware is needed in the Gateway, with allowed origins retrieved via Config Resolver or ENV.

**P13 -- Missing .dockerignore in Docker Builds**

Five Dockerfiles exist (api, gateway, worker, sandbox, browser), but no `.dockerignore` file is present in the repository. Docker build contexts will include `.env` (containing real keys), `.git/` (entire history), and `node_modules/`. Open-source users running `docker compose up --build` might unintentionally bake keys into images, while build times and image sizes swell unnecessarily.

**P14 -- Severely Insufficient Test Coverage**

Go: Only 67 out of 359 source files have corresponding test files (~18.7%); TypeScript: Only 7 out of 157 source files have test files (~4.5%). While P6 mentions the "lack of CI", CI is an execution issue, whereas test coverage is a code-level issue. The lack of tests for core paths means external contributors cannot determine if their PRs break existing behavior, leading to extremely high review costs.

**P15 -- Missing API Documentation**

Over 40 API endpoints (`/v1/auth/*`, `/v1/threads/*`, `/v1/runs/*`, `/v1/admin/*`, `/v1/orgs/*`, etc.) lack OpenAPI/Swagger specifications and independent API reference documentation. External developers must read Go handler source code to understand request/response contracts, authentication methods, and error codes. API documentation is basic developer experience for an open-source project.

**P16 -- Documentation Only in Chinese**

All documentation (README, architecture, roadmap, specs, guides) only exists in Chinese with `.zh-CN.md` suffixes. For an international open-source community, at least `README.md` (root), `CONTRIBUTING.md`, and the deployment guide (`deployment.md`) require English versions. If targeting only the Chinese community, the project scope must be explicitly stated in the README.

**P17 -- Outdated Historical Documentation**

`architecture-problems.zh-CN.md` was written when the system had no Redis, no Gateway, and no object storage. Several chapters describe problems already solved, but the document hasn't been updated. External readers seeing descriptions like "no Redis" or "incomplete user identity" will be seriously misled. This document must be updated before open-sourcing, marking the current status (resolved/partially resolved/persists) for each item.

**P18 -- Hardcoded Chinese in Persona Prompts**

`title_summarize.prompt` in Persona YAML contains hardcoded Chinese instructions (e.g., "Keep it under 8 Chinese characters"). Non-Chinese users will get unexpected title summaries. If Personas are community-extensible assets, prompt language should be configurable or support locale selection.

**P19 -- No Centralized Registration or Documentation for Error Codes**

API error codes (`invalid_credentials`, `email_not_verified`, `policy_denied`, `budget_exceeded`, `rate_limited`, etc.) are scattered across Go code in various modules, without centralized registration or enumeration documentation. External developers cannot predict all possible error types when writing clients, relying instead on trial and error or reading source code.

**P20 -- Unstated API Versioning Strategy**

The API uses the `/v1/` prefix, but no documentation explains compatibility commitments, deprecation policies, or handling of breaking changes. External integrators need to know if backward compatibility is guaranteed within `/v1/`, when `/v2/` might be introduced, and the notification period for deprecated endpoints.

---

## 1. Track A -- Unified Configuration System

**Goal**: Establish a single configuration resolution chain where all configuration points follow the same path and are manageable via the Console.

### A1 -- Config Resolver Core

Build a unified configuration resolver in `src/services/shared/config/`.

Resolution Chain (from highest to lowest priority):
1. ENV override (forced override at deployment layer)
2. DB org-level configuration (`org_settings` table, per-org customization)
3. DB platform-level configuration (`platform_settings` table, global default)
4. Default values registered in code (declared during Registry registration)

Core Interface:
```go
// shared/config/resolver.go
type Resolver interface {
    // Resolve a single key by scope
    Resolve(ctx context.Context, key string, scope Scope) (string, error)
    // Batch resolve all keys with a specified prefix by scope
    ResolvePrefix(ctx context.Context, prefix string, scope Scope) (map[string]string, error)
}

type Scope struct {
    OrgID *uuid.UUID // nil = platform scope
}
```

Implementation Points:
- Redis caching for DB query results (configurable TTL, active invalidation on write)
- ENV override always takes highest priority
- Resolution results include source tags (env/org_db/platform_db/default) for easier debugging

### A2 -- Config Registry (Declaration and Registration)

All configurable items must be registered before use, declaring the key, type, default value, description, and sensitivity:

```go
// shared/config/registry.go
type Entry struct {
    Key          string
    Type         string // "string" | "int" | "bool" | "duration"
    Default      string
    Description  string
    Sensitive    bool   // true = Original value not shown in Console, writes go to secrets table
    Scope        string // "platform" | "org" | "both"
}
```

Registration entries are placed in the `init` phase of each module, for example:
```go
config.Register(config.Entry{
    Key:     "email.smtp_host",
    Type:    "string",
    Default: "",
    Scope:   "platform",
})
```

The Registry also provides a metadata query interface (`GET /v1/config/schema`) for the Console to dynamically render configuration pages.

### A3 -- org_settings Table

Create a new migration:
```sql
CREATE TABLE org_settings (
    org_id  uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    key     text NOT NULL,
    value   text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, key)
);
```

Align with the existing `platform_settings` structure. The Resolver internally queries both tables.

### A4 -- Migrating Existing Configuration Consumers

Gradually migrate scattered configuration reading logic to the Resolver:

| Module | Current Method | Post-Migration |
|------|---------|--------|
| email | Custom config_db.go + ENV fallback | `config.ResolvePrefix(ctx, "email.", scope)` |
| web_search | Custom config_db.go + ENV fallback | `config.ResolvePrefix(ctx, "web_search.", scope)` |
| web_fetch | Custom config_db.go + ENV fallback | `config.ResolvePrefix(ctx, "web_fetch.", scope)` |
| openviking | Custom config.go + ENV fallback | `config.ResolvePrefix(ctx, "openviking.", scope)` |
| turnstile | Direct ENV reading | `config.Resolve(ctx, "turnstile.secret_key", scope)` |
| gateway rate limit | Mixed ENV + DB | `config.ResolvePrefix(ctx, "gateway.", scope)` |
| LLM retry | Direct ENV reading | `config.ResolvePrefix(ctx, "llm.retry.", scope)` |

Delete `config_db.go` in each module after migration.

### A5 -- Console Configuration Page Upgrade

Based on the A2 Registry schema interface, change the Console configuration page to dynamic rendering:
- Platform Settings: Global configuration items adjustable by platform admins
- Org Settings: Org-level overrides adjustable by org admins
- Sensitive values (Sensitive=true) stored via the `secrets` table, shown as masks in the Console

---

## 2. Track B -- Centralized System Limit Declaration

**Goal**: Register all system limits in a unified Registry, adjustable via the Console and documented.

### B1 -- Limits Registry

Extend the Track A Config Registry to include existing hardcoded limits:

| Key | Current Hardcoded Location | Default | Scope |
|-----|---------------|--------|-------|
| `limit.thread_message_history` | mw_input_loader.go | 200 | org |
| `limit.max_input_content_bytes` | v1_runs.go | 32768 | org |
| `limit.agent_reasoning_iterations` | mw_persona_resolution.go | 0 | org |
| `limit.tool_continuation_budget` | mw_persona_resolution.go | 32 | org |
| `limit.max_parallel_tasks` | lua.go | 32 | platform |
| `limit.concurrent_runs` | entitlement resolve.go | 10 | org |
| `limit.team_members` | entitlement resolve.go | 50 | org |
| `quota.runs_per_month` | entitlement resolve.go | 999999 | org |
| `quota.tokens_per_month` | entitlement resolve.go | 1000000 | org |
| `credit.initial_grant` | entitlement resolve.go | 1000 | platform |
| `credit.invite_reward` | entitlement resolve.go | 500 | platform |
| `credit.per_usd` | handler_agent_loop.go | 1000 | platform |
| `llm.max_response_bytes` | anthropic.go | 16384 | platform |
| `browser.max_body_bytes` | server.ts | 1048576 | platform |
| `browser.context_max_lifetime_s` | config.ts | 1800 | platform |
| `sandbox.idle_timeout_lite_s` | .env | 180 | platform |
| `sandbox.idle_timeout_pro_s` | .env | 300 | platform |
| `sandbox.idle_timeout_ultra_s` | .env | 600 | platform |
| `sandbox.max_lifetime_s` | .env | 1800 | platform |

### B2 -- Entitlement Integration with Resolver

Migrate hardcoded `defaults` map in `shared/entitlement/resolve.go` to the Config Registry. The Entitlement service reads plan definition -> org override -> platform default, aligning the chain with the Resolver.

### B3 -- Automatic Limit Documentation Generation

Automatically export markdown documentation from the Registry (all registered keys, types, defaults, scopes, descriptions) to `docs/reference/configuration.md`. CI checks if this file is synchronized with Registry code.

---

## 3. Track C -- Tool Provider Management (AS-11)

**Goal**: Support multi-backend registration for tools with the same name, platform defaults + per-org activation of Providers, and Console management of credentials and configuration.

This track corresponds to the full design of AS-11 in the agent-system-roadmap. Key milestones:

### C1 -- AgentToolSpec.LlmName + Multi-backend Registration (AS-11.1)
- Add `LlmName` field to `AgentToolSpec`
- Build LlmName -> Name reverse index in `DispatchExecutor.Bind()`
- Split web_search into `web_search.tavily` and `web_search.searxng`
- Split web_fetch into `web_fetch.jina`, `web_fetch.firecrawl`, and `web_fetch.basic`

### C2 -- DB Schema + Per-org Activation (AS-11.2)
- Create new `tool_provider_configs` table
- Add `scope` (`platform` / `org`)
- At most one `is_active = true` per scope + group_name
- Store sensitive values encrypted in the `secrets` table

### C3 -- Worker Pipeline Injection (AS-11.3)
- Create `mw_tool_provider.go`
- Insert after MCPDiscovery and before ToolBuild
- Read org-activated Provider from DB, falling back to platform-activated Provider if missing
- Override default executor

### C4 -- Console API + UI (AS-11.4 / AS-11.5)
- CRUD interfaces: list Provider Groups, activate/deactivate Providers, configure credentials
- Console page: Tool Provider Management (list + configuration)
- Connectivity testing: Not implemented

---

## 4. Track D -- Frontend Shared Layer

**Goal**: Web and Console share base code, eliminate duplication, and unify development paradigms.

### D1 -- Establishment of shared package

Create a shared package in `src/apps/shared/`, referenced via pnpm workspace:

```
src/apps/shared/
├── package.json          # @arkloop/shared
├── src/
│   ├── api/
│   │   ├── client.ts     # apiFetch, ApiError, token management
│   │   └── types.ts      # Shared types like LoginResponse, MeResponse
│   ├── storage/
│   │   └── tokens.ts     # access/refresh token read/write
│   ├── contexts/
│   │   ├── ThemeContext.tsx   # theme switching context + useTheme hook
│   │   └── LocaleContext.tsx  # language switching context + useLocale hook
│   ├── hooks/
│   │   └── useAuth.ts    # authentication state hook (if applicable)
│   └── index.ts
└── tsconfig.json
```

### D2 -- Migrating Duplicate Code from Web and Console

| Module | Web Current Location | Console Current Location | Post-Sharing Location |
|------|-------------|-----------------|-----------|
| apiFetch | `src/apps/web/src/api.ts` | `src/apps/console/src/api/client.ts` | `@arkloop/shared/api/client` |
| Type Definitions | `src/apps/web/src/api.ts` | `src/apps/console/src/api/*.ts` | `@arkloop/shared/api/types` |
| Token Management | `src/apps/web/src/storage.ts` | `src/apps/console/src/storage.ts` | `@arkloop/shared/storage/tokens` |
| ThemeContext | `src/apps/web/src/contexts/ThemeContext.tsx` | `src/apps/console/src/contexts/ThemeContext.tsx` | `@arkloop/shared/contexts/ThemeContext` |
| LocaleContext | `src/apps/web/src/contexts/LocaleContext.tsx` | `src/apps/console/src/contexts/LocaleContext.tsx` | `@arkloop/shared/contexts/LocaleContext` |

Migration Principle: Only migrate code confirmed as duplicate; avoid preemptive abstraction. Web and Console each import `@arkloop/shared` to replace local implementations.

Token storage constraints: Refresh Token is stored in an HttpOnly cookie (`arkloop_refresh_token`), and Access Token is kept in memory only. The shared storage module only provides in-memory access token read/write, and clears legacy `localStorage` keys at startup (`arkloop:web:access_token` / `arkloop:*:refresh_token`).

LocaleContext Note: `LocaleContext.tsx` code is identical (35 lines), but `LocaleStrings` interfaces and locale data (`locales/`) differ completely (Web ~200 keys, Console ~1070 keys). Sharing applies to the Context skeleton and `useLocale` hook, not the locale data itself. Each app continues maintaining its `locales/` directory, injecting its own `LocaleStrings` type via generics or re-export.

### D3 -- Establishment of pnpm workspace

Current Status: No `pnpm-workspace.yaml` exists in the root (only in `docs/` with `ignoredBuiltDependencies`, not a workspace config). Web and Console each hold independent `pnpm-lock.yaml` files as separate projects. Root `package.json` only contains `"web": "link:src/apps/web"`.

Migration Steps:

1. Create `pnpm-workspace.yaml` in root:
```yaml
packages:
  - src/apps/shared
  - src/apps/web
  - src/apps/console
```

2. Delete `src/apps/web/pnpm-lock.yaml` and `src/apps/console/pnpm-lock.yaml`. Run `pnpm install` in root to generate a unified lockfile.

3. Update root `package.json`, removing `"web": "link:src/apps/web"` (workspace handles package references automatically).

4. Add to `package.json` of Web and Console:
```json
"dependencies": {
  "@arkloop/shared": "workspace:*"
}
```

5. Check Vite configurations (`vite.config.ts`) to confirm if internal workspace package resolution and `optimizeDeps` require adjustment.

6. Check `tsconfig.json` to ensure `@arkloop/shared` type resolution is correct (may require `references` or `paths`).

---

## 5. Track E -- Unfinished Agent System Work

These AS-* items already have full design slices in the agent-system-roadmap; only status and priority are listed here.

### E1 -- Persona Routing Binding (AS-2.1)

Status: Unimplemented. Persona lacks the `preferred_credential` field; model selection depends entirely on external `route_id`.

Content: Add `preferred_credential` to Persona YAML; routing logic in `mw_routing.go` reads this field as a hint.

### E2 -- Memory Extraction Pipeline (AS-5.7)

Status: Unimplemented. `memory_search/read/write/forget` tools exist, but no automatic extraction flow follows a run.

Content: Trigger a lightweight LLM extraction of structured knowledge points after run completion to write into Memory. Triggered only if tool calls >= 2 or conversation turns >= 3.

### E3 -- Memory Testing (AS-5.8)

Status: Unimplemented.

Content: MemoryProvider interface tests + OpenViking adapter integration tests + Memory Tool end-to-end tests.

### E4 -- Cost Budget Enforcement on Execution Side (AS-8)

Status: Reserved fields exist; enforcement unimplemented. `RunContext.ToolBudget` field is available but no token consumption check exists within the Loop.

Content: Accumulate token consumption after each LLM call in `SimpleExecutor`, terminating with `budget.exceeded` if over limit.

### E5 -- Thinking Display Protocol (AS-10)

Status: Frontend `ThinkingBlock` component exists; backend lacks thinking channel separation and segment events.

Content:
- Sub-track A: Separate LLM native thinking output into `channel: "thinking"` events.
- Sub-track B: `run.segment.start/end` events for Agent-level execution process grouping.

### E6 -- Browser SSRF Protection (AS-7.5)

Status: Browser Service base functionality complete; SSRF protection unimplemented.

Content: Playwright route interception of internal addresses (RFC 1918/4193/6890) to block SSRF attack paths.

### E7 -- Scalability and Performance Baseline (AS-12)

Status: Unimplemented.

Content:
- AS-12.1: Browser Service horizontal scaling path (Session Affinity vs Stateless Mode decision)
- AS-12.2: Sandbox multi-node scheduling interface (SandboxClient abstraction)
- AS-12.3: Exposure of MCP Pool runtime metrics
- AS-12.4: OpenViking capacity baseline stress testing
- AS-12.5: Exposure of Worker DB connection pool configuration

### Track E.1 -- Docker Sandbox (non-Firecracker)

Status: Implemented. The Sandbox service, previously dependent on Firecracker (Linux/KVM), now includes a Docker-based backend for Mac/Windows (WSL2) development and OSS self-deployment.

Implementation:
- **Backend Selection**: Sandbox backend configured explicitly by admins via `ARKLOOP_SANDBOX_PROVIDER` or Console platform settings; no automatic inference.
- **Tool Exposure Names**:
  - LLM exposure names are `python_execute`, `exec_command`, and `write_stdin`
  - Provider display names used for backend/ops and canary: `python_execute.firecracker`, `python_execute.docker`, `exec_command.firecracker`, `exec_command.docker`
  - `write_stdin` reuses the same provider selected for `exec_command`, and only one sandbox provider is allowed per run.
- **Configuration (platform scope)**:
  - `sandbox.provider`: Default backend (`firecracker` / `docker`)
  - `sandbox.base_url`: Worker calls Sandbox service address (ENV still overrides)
  - `ARKLOOP_SANDBOX_DOCKER_IMAGE`: Container image for Docker backend (default `arkloop/sandbox-agent:latest`)
- **Internal Abstraction of Sandbox Service**:
  - Reuse `VMPool` interface (`Acquire`, `DestroyVM`, `Ready`, `Stats`, `Drain`), routing to different backends via config within the same service process.
  - `WarmPool` (Firecracker): Reuse warm pool + snapshot capabilities.
  - `docker.Pool` (Docker): Manage container lifecycle via Docker Engine API; container runs the same `sandbox-agent` listening in TCP mode, Sessions established via `Dialer` abstraction.
  - `Session.Dial` abstraction: Firecracker uses `vsock` dialer, Docker uses TCP dialer; upper-layer Exec/FetchArtifacts logic is identical.
  - Docker backend lacks Firecracker-style snapshot restore; pre-created containers in warm pool ensure response speed.
- **Security Hardening (Docker backend)**:
  - `--cap-drop=ALL`: Remove all Linux capabilities.
  - `--security-opt=no-new-privileges`: Prohibit privilege escalation.
  - `--pids-limit=256`: Limit process count.
  - CPU (NanoCPUs) and memory (Memory) limits set by tier.
  - Container ports bound to `127.0.0.1` random ports.
- **Acceptance**:
  - After switching `sandbox.provider`, new run `code_execute` follows the corresponding backend, fixed within the run.
  - No regression for Firecracker path on Linux; macOS/Windows (WSL2) passes `code_execute` and artifact upload via Docker.
  - Clean up session on run end; sandbox idle timeout handles Worker crashes.

---

## 6. Track F -- Plugin System (OpenCore / BusinessCore Separation)

**Goal**: Establish an architectural split between OpenCore (open-source core) and BusinessCore (commercial extensions). Commercial features (Stripe subscription, enterprise SSO, multi-channel notifications, advanced auditing, etc.) exist as independent Go modules in private repositories, integrating into the OSS core via compile-time registration without introducing runtime plugin loading.

**Core Motivation**: The open-source repository must be fully functional—without any commercial plugins, the system runs normally with built-in credits/JWT/Email. Commercial features provide enhancement, not essential functionality.

### F0 -- Design Decisions and Technical Selection

**Why not runtime plugin loading?**

Go's runtime plugin solutions have fundamental flaws:
- `plugin` package (.so): Requires strictly identical compiler and dependency versions, cross-platform unavailability, and community abandonment.
- HashiCorp go-plugin (independent process + gRPC): Suitable for CLI tools like Terraform, but introduces unnecessary IPC overhead and operational complexity for high-frequency calls on Agent Loop hot paths (billing, permission checks).

**Adopted Solution: `database/sql` style init() registration pattern**

A paradigm validated by the Go standard library—`import _ "github.com/lib/pq"` registers a driver in `init()`, used via `sql.Open("postgres", ...)`. Every Go developer understands this pattern.

Workflow:
1. OpenCore defines extension point interfaces and type-safe registries in `shared/plugin/`.
2. Each commercial plugin is an independent Go package, calling `plugin.RegisterXxx()` in `init()` to register itself.
3. OSS `main.go` only imports core code; BusinessCore `main.go` additionally imports commercial plugin packages.
4. Plugin inclusion decided at compile-time; zero runtime discovery/loading overhead.

### F1 -- Repository and Build Model

**Dual Repository Structure**:

```
arkloop/                           (Public Repository - OpenCore)
├── src/services/shared/plugin/    ← Extension point registries + interface definitions
│   ├── registry.go                ← Global registry (typed maps for each extension point)
│   ├── billing.go                 ← BillingProvider interface + OSS default implementation
│   ├── auth.go                    ← AuthProvider interface + OSS default implementation
│   ├── notify.go                  ← NotificationChannel interface + OSS default implementation
│   └── audit.go                   ← AuditSink interface + OSS default implementation
├── src/services/api/cmd/api/main.go       ← OSS entry (no commercial imports)
├── src/services/worker/cmd/worker/main.go ← OSS entry (no commercial imports)
└── ...

arkloop-enterprise/                (Private Repository - BusinessCore)
├── go.mod                         ← require arkloop OSS core as dependency
├── billing/
│   └── stripe/                    ← Stripe implementation for BillingProvider
│       ├── provider.go
│       └── webhook.go
├── auth/
│   └── oidc/                      ← OIDC/SAML implementation for AuthProvider
│       └── provider.go
├── notify/
│   ├── slack/                     ← Slack implementation for NotificationChannel
│   └── discord/                   ← Discord implementation for NotificationChannel
├── audit/
│   └── external/                  ← Enterprise-grade external output implementation for AuditSink
├── cmd/
│   ├── api/main.go                ← BusinessCore API entry
│   └── worker/main.go             ← BusinessCore Worker entry
└── ...
```

**BusinessCore Entry Example** (`arkloop-enterprise/cmd/api/main.go`):

```go
package main

import (
    api "arkloop.dev/services/api/cmd/api"

    // Commercial plugins (side-effect import, auto-registration in init())
    _ "arkloop.dev/enterprise/billing/stripe"
    _ "arkloop.dev/enterprise/auth/oidc"
    _ "arkloop.dev/enterprise/notify/slack"
)

func main() {
    api.Run()
}
```

The only difference from OSS `main.go` is a few extra import lines. The build product is a single binary without extra processes or .so files.

**Version Synchronization**: `arkloop-enterprise` `go.mod` references OSS core versions via Git tags; CI ensures compatibility between repositories.

### F2 -- Extension Point Registry

Establish a unified registration mechanism in `src/services/shared/plugin/`. Use independent typed registries for each extension point instead of a generic container.

```go
// shared/plugin/registry.go
package plugin

import "sync"

// Global registry, each extension point managed independently
var (
    mu sync.RWMutex

    billingProvider  BillingProvider
    authProviders    = map[string]AuthProvider{}
    notifyChannels   = map[string]NotificationChannel{}
    auditSinks       []AuditSink
)

// RegisterBillingProvider registers a billing implementation, overriding OSS default.
// Only one BillingProvider (Stripe or built-in) allowed per process.
func RegisterBillingProvider(p BillingProvider) {
    mu.Lock()
    defer mu.Unlock()
    billingProvider = p
}

// RegisterAuthProvider registers an authentication implementation. name is the provider ID (e.g., "oidc").
// Multiple can be registered, activated via configuration at runtime.
func RegisterAuthProvider(name string, p AuthProvider) {
    mu.Lock()
    defer mu.Unlock()
    authProviders[name] = p
}

// RegisterNotificationChannel registers a notification channel. name is the channel ID (e.g., "slack").
func RegisterNotificationChannel(name string, ch NotificationChannel) {
    mu.Lock()
    defer mu.Unlock()
    notifyChannels[name] = ch
}

// RegisterAuditSink registers an audit log output. Multiple can be registered; events written to all.
func RegisterAuditSink(s AuditSink) {
    mu.Lock()
    defer mu.Unlock()
    auditSinks = append(auditSinks, s)
}

// GetBillingProvider returns the active BillingProvider.
// Returns OSS built-in implementation if none registered.
func GetBillingProvider() BillingProvider { ... }
func GetAuthProvider(name string) (AuthProvider, bool) { ... }
func ListNotificationChannels() map[string]NotificationChannel { ... }
func GetAuditSinks() []AuditSink { ... }
```

The registry is frozen during the process startup phase (after `init()`, before service initialization in `main()`). It remains read-only at runtime, avoiding concurrent write risks.

### F3 -- Extension Point Interface Definitions

The following four interfaces constitute the OpenCore/BusinessCore split. Each has a full default implementation in OpenCore, which commercial plugins override via registration.

#### F3.1 -- BillingProvider (Billing)

Current Status: Credit deduction (`credits_repo.go`), subscription management (`subscriptions_repo.go`), and Plan resolution (`plans_repo.go`) directly operate on the DB. Logic is scattered across the handler layer and `entitlement.Service`.

```go
// shared/plugin/billing.go
type BillingProvider interface {
    // Subscription management
    CreateSubscription(ctx context.Context, orgID uuid.UUID, planID string) error
    CancelSubscription(ctx context.Context, orgID uuid.UUID) error
    GetActiveSubscription(ctx context.Context, orgID uuid.UUID) (*Subscription, error)

    // Usage synchronization (reporting token consumption after Agent run)
    ReportUsage(ctx context.Context, orgID uuid.UUID, usage UsageRecord) error

    // Quota check (called before run start to decide if execution is allowed)
    CheckQuota(ctx context.Context, orgID uuid.UUID, resource string) (allowed bool, err error)

    // Webhook handling (Stripe/Paddle callbacks)
    HandleWebhook(ctx context.Context, provider string, payload []byte, signature string) error
}

type UsageRecord struct {
    RunID       uuid.UUID
    TokensIn    int64
    TokensOut   int64
    ToolCalls   int
    DurationMs  int64
}
```

OSS Default Implementation: Wraps existing `credits_repo` + `subscriptions_repo` + `entitlement.Resolver` logic, maintaining current behavior.

Commercial Implementation Example: Stripe plugin calls Stripe API for subscription management, syncs usage to Stripe Metered Billing, and handles webhooks like `invoice.paid` / `customer.subscription.updated`.

Migration Path:
1. Consolidate direct handler calls to `credits_repo.Deduct()` / `subscriptions_repo.Create()` behind the `BillingProvider` interface.
2. Change token deduction after run end in Worker's `handler_agent_loop.go` to call `plugin.GetBillingProvider().ReportUsage()`.
3. Change entitlement checkpoints in API (before run start) to call `CheckQuota()`.

#### F3.2 -- AuthProvider (Auth Extensions)

Current Status: JWT double Token (HS256 signing/verification/refresh) + bcrypt password + Email OTP login. `auth.Service` is directly coupled with `JwtAccessTokenService` without an interface layer. RBAC uses static role mapping (`permissions.go`), but `RBACRolesRepository` reserves dynamic role capability.

AuthProvider does not replace JWT signing—JWT is an internal session token, always signed by OpenCore. AuthProvider extends "how user identity enters the system" via external IdP federated login.

```go
// shared/plugin/auth.go
type AuthProvider interface {
    // Name returns the provider ID (e.g., "oidc", "saml")
    Name() string

    // AuthCodeURL returns IdP login page URL (OAuth2 Authorization Code Flow)
    AuthCodeURL(ctx context.Context, state string) (string, error)

    // ExchangeToken uses IdP callback code to exchange for user identity
    ExchangeToken(ctx context.Context, code string) (*ExternalIdentity, error)

    // RefreshExternalToken refreshes token on IdP side (optional)
    RefreshExternalToken(ctx context.Context, refreshToken string) (*ExternalIdentity, error)
}

type ExternalIdentity struct {
    ProviderName string // "oidc", "saml", "github"
    ExternalID   string // Unique user ID on IdP side
    Email        string
    DisplayName  string
    AvatarURL    string
    RawClaims    map[string]any // Original IdP claims for audit and custom mapping
}
```

Federated Login Flow:
1. User visits `/v1/auth/sso/{provider}` -> Core calls `AuthCodeURL()` -> Redirect to IdP.
2. IdP callbacks `/v1/auth/sso/{provider}/callback?code=xxx` -> Core calls `ExchangeToken()`.
3. Core finds or creates local user after obtaining `ExternalIdentity` (`external_identities` table association).
4. Core signs the access_token via existing JWT logic, and issues/rotates the refresh token via the HttpOnly cookie `arkloop_refresh_token`; subsequent requests follow standard JWT verification.

OSS Default Implementation: SSO endpoint returns 404 if no external AuthProvider is registered. Existing username/password + Email OTP login remains unaffected.

Migration Path:
1. Add `external_identities` table (provider_name, external_id, user_id; composite unique).
2. Add `/v1/auth/sso/{provider}` and `/v1/auth/sso/{provider}/callback` handlers in API.
3. Call `plugin.GetAuthProvider(provider)` within handler; 404 if absent.

#### F3.3 -- NotificationChannel (Notification Channels)

Current Status:
- Email: `Mailer` interface (`Send(ctx, Message) error`), SMTP + NoOp implementations, asynchronous sending via Worker job queue.
- Webhook: Independent `webhook.DeliveryHandler`, HMAC-SHA256 signing, SSRF protection, exponential backoff retry.
- In-app Notification: `notifications` + `notification_broadcasts` tables, REST API query.

These three systems operate independently without unified scheduling.

```go
// shared/plugin/notify.go
type NotificationChannel interface {
    // Name returns channel ID (e.g., "slack", "discord", "telegram")
    Name() string

    // Send sends notification, returns delivery reference for tracking
    Send(ctx context.Context, notification Notification) (deliveryRef string, err error)
}

type Notification struct {
    EventType string         // e.g., "run.completed", "run.failed", "invite.received"
    OrgID     uuid.UUID
    Title     string
    Body      string
    Metadata  map[string]any // event-specific data
}
```

OSS Default Implementation: Notifications follow only Email (existing Mailer) and in-app routes if no NotificationChannel is registered. Webhooks remain independent.

Commercial Implementation Example: Slack plugin receives Notification, formats as Slack Block Kit message, and delivers via Incoming Webhook or Bot Token.

Notes:
- NotificationChannel is an outbound expansion point, not altering existing Email/Webhook/In-app logic.
- Channel activation and destination configuration (e.g., which Slack channel) follows Config Resolver (Track A), configurable per-org.
- Notification routing logic managed by Core scheduler, not delegated to plugins.

#### F3.4 -- AuditSink (Audit Log Output)

Current Status: No centralized audit log system. Operations are scattered in business logs of various handlers.

```go
// shared/plugin/audit.go
type AuditSink interface {
    // Name returns sink ID (e.g., "splunk", "datadog", "s3-archive")
    Name() string

    // Emit sends audit event. Implementations should be asynchronous.
    Emit(ctx context.Context, event AuditEvent) error
}

type AuditEvent struct {
    Timestamp time.Time
    ActorID   uuid.UUID      // Person who operated
    OrgID     uuid.UUID
    Action    string         // e.g., "user.login", "run.create", "apikey.rotate"
    Resource  string         // Resource type operated upon
    ResourceID string        // ID of the resource
    Detail    map[string]any // Details
    IP        string
    UserAgent string
}
```

OSS Default Implementation: `DBSink`—writes audit events to `audit_events` table (new migration), providing basic query API.

Commercial Implementation Example: Splunk/Datadog plugin pushes events asynchronously to enterprise SIEM systems.

AuditSink allows multiple registrations; events are written to all registered sinks. OSS `DBSink` always exists; commercial sinks are additional output channels.

### F4 -- Plugin Configuration Integration

Plugin runtime configuration follows Config Resolver (Track A), introducing no new configuration paths.

**Declaration on Registration**: Each plugin calls `config.Register()` in `init()` to register its required configuration keys:

```go
// arkloop-enterprise/billing/stripe/provider.go
func init() {
    config.Register(config.Entry{
        Key:       "billing.stripe.secret_key",
        Type:      "string",
        Sensitive: true,
        Scope:     "platform",
    })
    // ...
    plugin.RegisterBillingProvider(&StripeProvider{})
}
```

Console configuration management page (Track A5) obtains all registered items via `GET /v1/config/schema`; keys registered by commercial plugins automatically appear in the Console without frontend changes.

**Plugin Activation Configuration**: Keys in Config Resolver control which implementation is active:

| Key | Meaning | Default |
|-----|------|--------|
| `plugin.billing.provider` | Billing implementation (`builtin` / `stripe`) | `builtin` |
| `plugin.auth.sso.enabled` | Whether SSO login is enabled | `false` |
| `plugin.auth.sso.provider` | SSO implementation (`oidc` / `saml`) | empty |
| `plugin.notify.channels` | Activated notification channels (comma-separated) | empty |
| `plugin.audit.sinks` | Activated audit sinks (comma-separated) | `db` |

### F5 -- OSS Default Implementation and Degradation Guarantee

Each extension point must have an OpenCore built-in implementation to ensure the system remains fully functional without commercial plugins:

| Extension Point | OSS Default Implementation | Behavior |
|--------|-------------|------|
| BillingProvider | `BuiltinBillingProvider` | Wraps credits_repo + subscriptions_repo + entitlement logic |
| AuthProvider | None registered | SSO endpoint returns 404; existing login methods unaffected |
| NotificationChannel | None registered | Notifications follow Email + In-app + Webhook (current logic) |
| AuditSink | `DBSink` | Audit events written to local DB, queryable in Console |

**Degradation Rules**:
- If a commercial plugin is registered but configuration is missing (e.g., empty Stripe `secret_key`): Log warning at startup; runtime calls return a clear error (`ErrProviderNotConfigured`); no panic or silent degradation to OSS implementation.
- This is because administrators explicitly chose the commercial implementation; silent degradation would mask configuration issues.

### F6 -- Inventory of Existing Interfaces (Non-Plugin Boundaries)

The following interfaces already exist and serve as good internal abstractions, but are **not OpenCore/BusinessCore split lines**:

| Interface | Location | Description |
|------|------|------|
| `llm.Gateway` | worker/llm/gateway.go | LLM call abstraction. Extensions handled via Track C and routing. |
| `VMPool` | sandbox/session/manager.go | Sandbox VM pool. Firecracker can be replaced, but it's a deployment choice. |
| `SnapshotStore` | sandbox/storage/store.go | Snapshot storage. MinIO implementation is S3 compatible. |
| `Mailer` | worker/email/mailer.go | Email sending. Sub-set of NotificationChannel. |
| `MemoryProvider` | worker/memory/provider.go | Memory system. OpenViking implementation, fail-open design. |
| `config.Resolver` | shared/config/resolver.go | Configuration resolution. Infrastructure, not a plugin. |
| `tools.Executor` | worker/tools/ | Tool execution. MCP covers dynamic expansion. |
| `executor.Registry` | worker/executor/ | Agent executor registration. Factory pattern for internal use. |

Extensions to these (e.g., adding a new LLM Provider) are done via existing registration/configuration mechanisms, not the `shared/plugin/` registry.

### F7 -- Database Migrations

Schema changes required for the plugin system:

```sql
-- F3.2: External identity association (SSO)
CREATE TABLE external_identities (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_name text NOT NULL,
    external_id   text NOT NULL,
    email         text,
    display_name  text,
    raw_claims    jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider_name, external_id)
);

-- F3.4: Audit events (used by OSS DBSink)
CREATE TABLE audit_events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    timestamp   timestamptz NOT NULL DEFAULT now(),
    actor_id    uuid REFERENCES users(id),
    org_id      uuid REFERENCES orgs(id),
    action      text NOT NULL,
    resource    text NOT NULL,
    resource_id text,
    detail      jsonb,
    ip          text,
    user_agent  text
);
```

These migrations belong to OpenCore (serving default implementations) and are submitted alongside Track F.

---

## 7. Track G -- Infrastructure Development

**Goal**: Establish quality gates and performance baselines to ensure code quality after open-sourcing.

### G1 -- CI Pipeline

GitHub Actions configuration triggered by PRs and main branch pushes.

**Go Services**:
- `go vet ./...` + `staticcheck ./...` (static analysis)
- `go test ./...` (unit tests + coverage reports)
- `go build ./...` (compilation checks)
- Runs independently for api, gateway, worker, sandbox, and shared modules.

**TypeScript Frontend**:
- `pnpm lint` (ESLint)
- `pnpm type-check` (tsc --noEmit)
- `pnpm test` (Vitest)
- Runs independently for web, console, browser, and shared.

**Database**:
- Migration forward/rollback testing (apply all -> rollback all -> reapply).

### G2 -- Stress Testing Baseline

Establish single-node capacity limits for each service using k6 or Go bench:

| Target | Concurrency | Metric |
|------|------|------|
| Gateway Rate Limit Throughput | 1000 req/s | P99 latency < 10ms |
| API CRUD | 200 concurrency | P99 latency < 100ms |
| SSE Long Connections | 500 concurrency | Connection retention > 99% |
| Worker Agent Loop | 50 concurrent runs | DB connection pool non-overflow |
| OpenViking Retrieval | 100 concurrency | P99 latency < 500ms |
| Browser concurrent sessions | 20 concurrency | Memory < 4GB |

Bench scripts in `tests/bench/`, results recorded in `docs/benchmark/`.

Delivered:
- Scripts: `tests/bench/` (`go run ./tests/bench/cmd/bench baseline`)
- Dedicated environment: `compose.bench.yaml` (ports offset +5; Worker defaults to stub)
- Baseline results: `docs/benchmark/baseline-2026-03-03.json`
- OpenViking scenarios excluded from baseline suite by default.

### G3 -- One-Click Development Environment Setup

Ensure `docker compose up` + minimal ENV configuration starts the full dev environment. Verification checklist:
- `.env.example` covers all necessary configurations.
- `compose.yaml` includes health checks for all services.
- Migrations run automatically.
- Optional seed data injection (admin account + example org).

### G4 -- Gateway CORS Middleware

Gateway currently lacks CORS handling. Separation of frontend and backend (almost all dev and prod scenarios) will be blocked by browsers.

Implementation:
- Add CORS handler in Gateway middleware chain, after `traceMiddleware` and before proxy.
- Allowed Origins via ENV (`ARKLOOP_CORS_ALLOWED_ORIGINS`), defaulting to `*` (dev mode); production requires explicit declaration.
- Handle preflight (OPTIONS) requests, correctly returning `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers`, and `Access-Control-Allow-Credentials`.
- SSE endpoints (`/v1/runs/*/events`) must allow `text/event-stream` Accept header.

### G5 -- .dockerignore

Add `.dockerignore` for all Dockerfile directories (or root), excluding:
```
.env
.env.*
!.env.example
.git
.github
node_modules
*.test
*.exe
docs/investor-deep-research.md
```
Prevents sensitive files (`.env`), irrelevant files (`.git/`, `node_modules/`) from entering images, while reducing build context size.

### G6 -- Core Path Test Completion

Current test coverage (Go ~18.7%, TS ~4.5%) is insufficient for external contribution safety. Completion by risk priority:

**Priority 1 (Security and Correctness)**:
- Auth chain: JWT issuance/verification/refresh/expiration, RBAC permission checks, API Key validation.
- Config resolution: Resolver multi-level fallback, ENV override, missing key handling.
- Tool security: URL policy (SSRF interception), Webhook delivery internal blocking.
- Encryption: Envelope encrypt/decrypt symmetry, key rotation.

**Priority 2 (Core Business)**:
- Pipeline middleware chain: message loading, Persona resolution, routing selection, Budget check.
- SSE pushing: event sequence correctness, resumption (`after_seq`).
- Entitlement resolution: plan -> org -> platform fallback.

**Priority 3 (Integration Verification)**:
- Migration forward/rollback consistency (covered in G1).
- `compose` full-stack smoke tests (API health + org creation + starting run).

Target is not 100% coverage, but rather "core paths protected by tests, allowing CI to judge if external PRs break existing behavior".

---

## 8. Track H -- Open Source Release and Governance (Repository Standards)

**Goal**: Complete open-source repository baselines for compliance, governance, release, and security, ensuring external users can run it out of the box, locate issues, and contribute effectively.

### H1 -- Open Source Boundary and Repository Hygiene [DONE]

- [x] Define open-source scope: `docs/OPEN-SOURCE-BOUNDARY.md` lists categories for OSS core, config templates, and exclusions.
- [x] Establish pre-release cleanup checklist: git history key scanning (passed), `.dockerignore` creation, personal path cleanup.
- [x] Change "internal" documentation labels to external context (`docs/index.md` hero text), update `.gitignore`, delete unsuitable files.

### H2 -- Licensing and Third-Party Dependency Compliance [DONE]

- [x] Select and implement primary license: `LICENSE` in root (Arkloop License = modified Apache 2.0 + multi-tenant restriction + brand protection) + `NOTICE`.
- [x] Establish third-party dependency license list: `docs/THIRD-PARTY-LICENSES.md` (159 Go modules + 31 npm packages, all permissive licenses, no copyleft risk).
- [ ] Define trademark/project name usage rules (minimal: permitted/prohibited scenarios) to avoid disputes -- suggest inclusion in H3 `CONTRIBUTING.md`.

### H3 -- Community Standard Files and Contribution Process

- Complete community standard files in root: `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `SUPPORT.md`.
- Complete collaboration entries: Issue templates, PR templates, minimal reproduction templates (required fields for bug reports).
- Define version compatibility commitments (SemVer / breaking change policy / deprecation cycles).

### H4 -- Release and Upgrade Strategy

- Release process: tag specifications, `CHANGELOG.md` generation strategy, release product (Docker images/binaries) list.
- Upgrade guide: forward/rollback boundaries for DB migrations, `UPGRADE.md` (covering at least major version upgrades).

### H5 -- Security and Supply Chain Baseline (CI Integration)

- Secret scanning: Scan PRs for keys/private keys in CI (require local pre-commit optionally).
- Dependency risk: Go `govulncheck`, Node `pnpm audit`, making high-risk vulnerabilities release-blocking.
- Image and SBOM: Image vulnerability scanning for Dockerfile build products, generate SBOM for traceability.

### H6 -- Deployment Profile and Feature Matrix

- Provide "minimal runnable set" deployment: Set Sandbox/Browser/OpenViking as optional profiles, ensuring macOS/Windows can at least run API/Gateway/Web/Console.
- Output feature matrix: Which tools are available and which capabilities degrade under different profiles.

### H7 -- API Documentation / OpenAPI Spec

Current 40+ API endpoints lack machine-readable specifications.

Implementation (pick one):
- Generate OpenAPI 3.0 spec from Go handler code (using `swaggo/swag` or manual YAML maintenance).
- Place spec in `docs/api/openapi.yaml`, verify consistency with code in CI.
- Generate static API reference docs based on spec (Redoc / Scalar / Stoplight Elements).

Minimum Delivery: Cover five core endpoint groups: `/v1/auth/*`, `/v1/threads/*`, `/v1/runs/*`, `/v1/orgs/*`, and `/v1/me`. Each includes path, method, request body schema, response schema, authentication method, and error code enumeration.

### H8 -- Documentation Internationalization

Current documentation is only in Chinese. Language strategy required before open-sourcing:

**Option A (Targeting International Community)**:
- Root `README.md` in English, `README.zh-CN.md` for Chinese.
- `CONTRIBUTING.md`, `SECURITY.md`, and deployment guide in English.
- Remaining engineering docs (architecture, roadmap, specs) remain in Chinese, noted in the English README.

**Option B (Targeting Chinese Community Only)**:
- Prominently state in README: `This project's documentation is currently available in Chinese (Simplified) only.`
- Welcome community translation PRs.

Regardless of option, root `README.md` requires rewrite (current content is only an architecture outline, lacking project intro, quick start, screenshots/demo, tech stack, license statement, etc.).

### H9 -- Historical Documentation Cleanup

`architecture-problems.zh-CN.md` was written in an early stage; several issues described have been resolved (Gateway established, Redis/MinIO integrated, user identity refined, content_json supported in messages, run table lifecycle fields added, event table monthly partitioning implemented, Teams/Projects implemented, etc.).

Handling:
- Mark current status for each item (resolved / partially resolved / persists), attaching corresponding migration or commit reference.
- Or add archival statement at the top: explain writing timestamp, state that the repository no longer reflects the status described, and guide readers to `architecture-design-v2.md`.

### H10 -- Error Code Registration and Documentation

API error codes are scattered; centralization is required:
- Establish error code constant registration in `src/services/shared/apierr/` (some basis exists).
- Each code includes: code string, HTTP status, description, trigger scenario.
- Automatically export error code reference documentation to `docs/reference/error-codes.md`.
- Reference these codes in OpenAPI spec (协同 with H7).

### H11 -- API Versioning Strategy Declaration

Declare API versioning strategy in `CONTRIBUTING.md` or a standalone doc:
- Backward compatibility guaranteed within `/v1/`.
- Breaking changes require deprecation tagging one release cycle in advance.
- Client notification of endpoint deprecation via response headers (`Deprecation`, `Sunset`).
- When to consider `/v2/`: When compatibility constraints in `/v1/` block core architectural evolution.

### H12 -- Persona Prompt Language Configuration

The `title_summarize.prompt` in Persona YAML currently has hardcoded Chinese instructions. Handling:
- `title_summarize.prompt` supports locale selection (read from thread/org user preferences).
- Default Persona YAML provides both English and Chinese prompt templates.
- Or move title_summarize prompt to Config Registry (Track A), allowing deployers to configure.

---

## 9. Execution Priority and Dependency Relationships

```
Track A (Configuration) — Highest priority, foundation for all tracks
  A1 → A2 → A3 → A4 → A5

Track B (System Limits) — Depends on A1/A2
  B1 → B2 → B3

Track C (Tool Provider) — Independent, can run parallel with A
  C1 → C2 → C3 → C4

Track D (Frontend Shared) — Independent, can run parallel with A/C
  D3 (workspace) → D1 (shared package) → D2 (migration)

Track E (Agent System Unfinished) — Items independent
  E1 (Persona routing)
  E2 → E3 (Memory extraction → testing)
  E4 (Cost Budget)
  E5 (Thinking protocol)
  E6 (Browser SSRF)
  E7 (Performance baseline, depends on E6)

Track F (Plugin System / OpenCore-BusinessCore) — Depends on Track A
  F0 (Design)
  F2 (Registry) → F3 (Interface + OSS default) → F4 (Config integration, depends on A2) → F7 (DB migration)
  F1 (Repo model, parallel with F2)
  F5 (Degradation guarantee, depends on F3)
  F6 (Interface inventory, documentation, independent)

Track G (Infrastructure) — Independent, suggest starting with A
  G1 (CI)
  G2 (Stress test, depends on E7)
  G3 (Dev environment)
  G4 (CORS middleware) — MUST complete before open-source
  G5 (.dockerignore) — MUST complete before open-source
  G6 (Test completion) — Depends on G1, ongoing

Track H (Release and Governance) — Parallel with A/G, must converge before release
  H1 → H2 → H3 → H4 → H5 → H6
  H7 (API doc) — Independent, parallel with H3
  H8 (Internationalization) — Depends on H3
  H9 (Historical doc cleanup) — Independent
  H10 (Error codes) — Coordinated with H7
  H11 (API versioning) — Depends on H3/H7
  H12 (Persona prompt i18n) — Depends on Track A
```

**Recommended Execution Order**:

First Batch (Parallel):
- Track A (A1-A3): Configuration core
- Track D (D1-D3): Frontend shared layer
- Track G (G1, G3, G4, G5): CI + Dev environment + CORS + .dockerignore
- Track H (H1-H3, H9): Open source boundary + Licensing + Community files + Historical doc cleanup

Second Batch (After A1-A3):
- Track A (A4-A5): Config migration
- Track B (B1-B3): System limits
- Track C (C1-C4): Tool Provider
- Track G (G6): Core path test completion
- Track H (H7, H8, H10): API documentation + Internationalization + Error codes

Third Batch (As needed):
- Track E items (sorted by product priority)
- Track F (F0-F2 parallel with second batch; F3-F7 after Track A; commercial plugin in independent repo)
- Track G (G2 Stress test)
- Track H (H4-H6, H11, H12): Release strategy/Supply chain/Deployment profile/API versioning/Persona i18n (MUST complete before release)

---

## 10. Invariants and Decision Records

The following decisions are fixed within this roadmap:

- **Config Resolution Chain Fixed**: ENV override > org_settings DB > platform_settings DB > code defaults. No new config sources allowed.
- **All Configs Must Be Registered**: Resolving unregistered keys returns an error; no "silent" reading of undeclared configs.
- **Scope Model Fixed**: Platform and org levels only. No user-level or team-level configs (over-engineering). Thread/project level configs follow AgentConfig inheritance (existing mechanism), not Config Resolver.
- **Frontend Shared Package Only Includes Confirmed Duplicates**: No preemptive abstraction, no UI component library. Web and Console UI layers remain independent.
- **Plugin System Uses Compile-Time Registration**: Go `init()` + typed registry; no runtime plugin loading (.so, gRPC go-plugin). Commercial code in separate private repo via `import _` side-effect registration. No commercial code in OSS core.
- **Plugin Boundary Limited to Four Extension Points**: BillingProvider, AuthProvider, NotificationChannel, AuditSink. Existing interfaces (llm.Gateway, VMPool, etc.) are internal abstractions, not plugin registry points.
- **Full Functionality Without Plugins**: All extension points have OSS default implementations. Commercial plugins enhance, they don't restore essential features.
- **CI Does Not Block Development**: CI failures generate warnings, not merge blocks (switching to enforcement before release).
- **Old Roadmaps Archived, Not Deleted**: Three old roadmaps kept for historical reference, no new content. New work tracked here.
- **Browser SSRF Must Complete Before Release**: Non-negotiable security baseline.
- **Existing Decisions Continued**: All invariants in Section 16 of agent-system-roadmap remain in effect (Sandbox independent service, Executor registry, Memory fail-open, Model priority chain, Sub-agent level limits, Thinking protocol, Browser Service independent deployment, Tool Provider dual-names, Lua Executor choice, etc.).
- **Open Source Repository Standard Files Required**: `LICENSE`, `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md` are prerequisites for release.
- **Minimal Runnable Set Priority**: Default "minimal deployment profile" must not require KVM/privileged containers; high-dependency capabilities like Sandbox must be explicitly enabled.
- **CORS Must Complete Before Release**: Essential for separate frontend-backend deployment.
- **.dockerignore Must Exist Before Release**: Prevents `.env` and `.git/` from entering images.
- **API Documentation Covers Core Endpoints**: OpenAPI spec for at least auth, threads, runs, orgs, and me groups upon release.
- **Error Codes Must Be Enumerable**: New error codes must be registered in centralized registry; no direct construction of unregistered code strings in handlers.
- **Historical Docs Marked, Not Deleted**: Documents like `architecture-problems.zh-CN.md` are kept but marked with current status (resolved/persists) for each item.
