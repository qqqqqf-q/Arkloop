---
---

# Deployment Guide

Arkloop orchestrates all services via `compose.yaml`, enabling a full deployment with a single command.

## Service Overview

| Service | Description | Default Port |
|------|------|---------|
| `postgres` | PostgreSQL 16 | 5432 |
| `pgbouncer` | Connection Pool | 5433 |
| `redis` | Cache/Queue | 6379 |
| `seaweedfs` | S3-compatible object storage | 9000 |
| `migrate` | Database Migrations (One-time, exits after completion) | — |
| `api` | Control Plane API (Go) | 8001 |
| `gateway` | Reverse Proxy + Rate Limiting | 8000 |
| `worker` | Job Worker (Agent Execution Plane) | — |
| `sandbox` | Code Sandbox (Firecracker / Docker) | 8002 |
| `openviking` | Vector Memory Service | 1933 |

Startup order is guaranteed by `depends_on`: postgres → pgbouncer → migrate → api/worker → gateway, redis → api/gateway/worker, and seaweedfs → api/worker/sandbox.

## Quick Start

### 1. Prepare Environment Variables

```bash
cp .env.example .env
```

Edit `.env` and set at least the following required fields:

| Variable | Description |
|------|------|
| `ARKLOOP_POSTGRES_PASSWORD` | PostgreSQL Password |
| `ARKLOOP_AUTH_JWT_SECRET` | JWT Signing Secret (at least 32 characters) |
| `ARKLOOP_ENCRYPTION_KEY` | AES-256-GCM Key (32-byte hex) |
| `ARKLOOP_STORAGE_BACKEND` | Storage backend, defaults to `filesystem` |
| `ARKLOOP_STORAGE_ROOT` | Local storage root directory |

Generate secure keys:

```bash
# JWT Secret (at least 32 characters)
openssl rand -base64 48

# Encryption Key (32-byte hex)
openssl rand -hex 32
```

### 2. Start All Services

```bash
docker compose up -d
```

The default compose stack now uses local `filesystem` storage, which fits single-node deployments. If you switch to SeaweedFS or another S3-compatible backend, set `ARKLOOP_STORAGE_BACKEND=s3` and provide the `ARKLOOP_S3_*` variables explicitly.

The `migrate` service will automatically run migrations before `api/worker` starts and then exit. Check startup status:

```bash
docker compose ps
```

### 3. Access Services

| Endpoint | Description |
|------|------|
| `http://localhost:8000` | Public entry point (with Gateway rate limiting/auth) |

Internal services stay on the Docker network by default. If you need host-level debugging ports, start with the development override file:

```bash
docker compose -f compose.yaml -f compose.dev.yaml up -d
```

## Platform Administrator Initialization

On initial deployment, you can bootstrap a `platform_admin` user via an environment variable (one-time).

Steps:
1. Register / log in with an account
2. Call `GET /v1/auth/me` to get the `id`
3. Set in `.env`:

```bash
ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN=<user_id>
```

Then restart the `api` service.

## Tool Providers (Optional)

Tools like `web_search` and `web_fetch` require backend Provider and credential configuration.

Recommended approach (Universal for SaaS / Self-hosting):
- Log into Console using the bootstrap `platform_admin`.
- Configure Tool Providers with `scope=platform` as global defaults.
- For individual tenant customization, override with `scope=org`.

Compatibility mode (Local quick start only):
- Configure legacy `web_search.*` / `web_fetch.*` via environment variables (e.g., `ARKLOOP_WEB_SEARCH_PROVIDER`, `ARKLOOP_WEB_SEARCH_TAVILY_API_KEY`).

## View Logs

```bash
# All services
docker compose logs -f

# Specific service
docker compose logs -f api
docker compose logs -f worker
docker compose logs -f gateway
```

## Redeploy (After Code Update)

```bash
docker compose build api worker gateway
docker compose up -d
```

Migrations will automatically re-run before `api` starts. To force a manual migration run:

```bash
docker compose run --rm migrate up
```

## Stop / Clean Up

```bash
# Stop, keep data
docker compose down

# Stop and remove volumes (reset database)
docker compose down -v
```

## Sandbox rootfs Build

The Sandbox service uses Firecracker microVMs for user code execution, requiring a pre-built rootfs ext4 image.

### Build rootfs

```bash
cd src/services/sandbox/rootfs
./build.sh
```

Build artifacts are output to `src/services/sandbox/rootfs/output/python3.12.ext4`.

### Deploy to Target Machine

```bash
DEPLOY_HOST=user@host ./build.sh
```

Default deployment path is `/opt/sandbox/rootfs/`, override via `DEPLOY_PATH`.

### Pre-installed Environment

| Category | Content |
|------|------|
| Python 3.12 | numpy, pandas, matplotlib, scipy, sympy, pillow, scikit-learn, requests, httpx, beautifulsoup4, lxml, openpyxl, pyyaml, rich |
| Node.js 20 | node, npm, npx |
| System Tools | busybox, curl, git, jq, sqlite3 |

Modify pre-installed content: edit `src/services/sandbox/rootfs/Dockerfile.python3.12` and rebuild.

## Sandbox Deployment

Sandbox supports two backend Providers, switched via `sandbox.provider` configuration (or `ARKLOOP_SANDBOX_PROVIDER` environment variable).

### Firecracker Mode (Default)

Linux + KVM environment, using microVM isolation:

```bash
docker compose --profile firecracker up -d sandbox
```

Requires `/dev/kvm` device and Firecracker binary.

Run it with least privilege; `privileged` is no longer required.

### Docker Mode

macOS / Windows (WSL2) / No KVM environment, using Docker container isolation:

```bash
# Point to a user-scoped Docker socket
export ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH=/run/user/1000/docker.sock

# Start
docker compose --profile docker-sandbox up -d sandbox-docker
```

Compose will build the local `sandbox-agent` image from `src/services/sandbox/Dockerfile.agent` and tag it as `arkloop/sandbox-agent:dev` by default.

On Linux, prefer the rootless Docker user socket.
On macOS / Windows Docker Desktop, use the socket under the user's home directory instead of the system-level `/var/run/docker.sock`.

`sandbox-docker` itself stays on the Compose networks, while spawned `sandbox-agent` containers join the internal `arkloop_sandbox_agent` network. Host networking is not used.

### Local Development (Direct Run)

```bash
cd src/services/sandbox
go build -o sandbox-bin ./cmd/sandbox

# Docker Mode
ARKLOOP_SANDBOX_PROVIDER=docker \
DOCKER_HOST=unix:///run/user/1000/docker.sock \
ARKLOOP_SANDBOX_SOCKET_DIR=/tmp/sandbox \
ARKLOOP_SANDBOX_TEMPLATES_PATH="" \
./sandbox-bin
```

### Sandbox Configuration

Runtime parameters are configured via Console > Configuration > Sandbox page (written to `platform_settings` table), or can be overridden with environment variables:

| Config Key | Env Var | Default | Description |
|---|---|---|---|
| `sandbox.provider` | `ARKLOOP_SANDBOX_PROVIDER` | `firecracker` | Backend type |
| `sandbox.allow_egress` | `ARKLOOP_SANDBOX_ALLOW_EGRESS` | `true` | Whether Sandbox backends may access the public network |
| `sandbox.docker_image` | `ARKLOOP_SANDBOX_DOCKER_IMAGE` | `arkloop/sandbox-agent:dev` | Docker agent image used for local Docker backend runs |
| `sandbox.max_sessions` | `ARKLOOP_SANDBOX_MAX_SESSIONS` | `50` | Max concurrent sessions |
| `sandbox.boot_timeout_s` | `ARKLOOP_SANDBOX_BOOT_TIMEOUT_S` | `30` | Boot timeout (seconds) |
| `sandbox.warm_lite` | `ARKLOOP_SANDBOX_WARM_LITE` | `3` | Pre-warmed lite instances |
| `sandbox.warm_pro` | `ARKLOOP_SANDBOX_WARM_PRO` | `2` | Pre-warmed pro instances |
| `sandbox.warm_ultra` | `ARKLOOP_SANDBOX_WARM_ULTRA` | `1` | Pre-warmed ultra instances |
| `sandbox.idle_timeout_lite_s` | `ARKLOOP_SANDBOX_IDLE_TIMEOUT_LITE_S` | `180` | Lite idle timeout (seconds) |
| `sandbox.idle_timeout_pro_s` | `ARKLOOP_SANDBOX_IDLE_TIMEOUT_PRO_S` | `300` | Pro idle timeout (seconds) |
| `sandbox.idle_timeout_ultra_s` | `ARKLOOP_SANDBOX_IDLE_TIMEOUT_ULTRA_S` | `600` | Ultra idle timeout (seconds) |
| `sandbox.max_lifetime_s` | `ARKLOOP_SANDBOX_MAX_LIFETIME_S` | `1800` | Max lifetime (seconds) |

Deployment-level parameters (ENV only, not in Console):

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_SANDBOX_ADDR` | `0.0.0.0:8002` | Service listener address |
| `ARKLOOP_FIRECRACKER_BIN` | `/usr/bin/firecracker` | Firecracker binary path |
| `ARKLOOP_SANDBOX_KERNEL_IMAGE` | `/opt/sandbox/vmlinux` | Kernel image path |
| `ARKLOOP_SANDBOX_ROOTFS` | `/opt/sandbox/rootfs.ext4` | rootfs path |
| `ARKLOOP_SANDBOX_SOCKET_DIR` | `/run/sandbox` | Temp file directory |
| `ARKLOOP_SANDBOX_TEMPLATES_PATH` | `/opt/sandbox/templates.json` | Template file path |
| `ARKLOOP_SANDBOX_EGRESS_INTERFACE` | `eth0` | Firecracker NAT uplink interface |
| `ARKLOOP_SANDBOX_FIRECRACKER_TAP_PREFIX` | `arktap` | Firecracker TAP name prefix |
| `ARKLOOP_SANDBOX_FIRECRACKER_TAP_CIDR` | `172.29.0.0/16` | Firecracker TAP address pool |
| `ARKLOOP_SANDBOX_FIRECRACKER_DNS` | `1.1.1.1,8.8.8.8` | Firecracker guest DNS servers |
| `ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH` | - | Required for the `docker-sandbox` profile; path to the host user-scoped Docker socket |

## Local Development Mode

During development, you typically run the API on the host machine (for debugging/hot-reloading) while infrastructure runs in Docker:

```bash
# Start infrastructure only
docker compose -f compose.yaml -f compose.dev.yaml up -d postgres redis seaweedfs pgbouncer

# Run migrations
cd src/services/api
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/migrate up

# Run API on host
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/api
```

If you need to use the Gateway, point upstream to the host:

```bash
ARKLOOP_GATEWAY_UPSTREAM=http://host.docker.internal:8001 docker compose -f compose.yaml -f compose.dev.yaml up -d gateway
```

## Full Environment Variable Reference

### Database

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_DATABASE_URL` | — | Main connection string (points to PgBouncer in production) |
| `ARKLOOP_DATABASE_DIRECT_URL` | — | Direct DSN (reserved for SSE LISTEN/NOTIFY) |
| `ARKLOOP_POSTGRES_USER` | `arkloop` | |
| `ARKLOOP_POSTGRES_PASSWORD` | — | Required |
| `ARKLOOP_POSTGRES_DB` | `arkloop` | |
| `ARKLOOP_PGBOUNCER_POOL_SIZE` | `200` | PgBouncer pool size |
| `ARKLOOP_PGBOUNCER_MAX_CLIENT_CONN` | `1000` | PgBouncer max client connections |

### Redis

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_REDIS_URL` | — | Redis connection string |
| `ARKLOOP_REDIS_PASSWORD` | `arkloop_redis` | |

### Storage

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_STORAGE_BACKEND` | `filesystem` | Default for local deploys; use `s3` for multi-node setups |
| `ARKLOOP_STORAGE_ROOT` | `/var/lib/arkloop/storage` | Root directory for the `filesystem` backend |

### Object Storage (Optional SeaweedFS / S3-Compatible)

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_S3_ENDPOINT` | — | Endpoint URL for the `s3` backend |
| `ARKLOOP_S3_ACCESS_KEY` | `arkloop` | |
| `ARKLOOP_S3_SECRET_KEY` | — | Required for the `s3` backend |
| `ARKLOOP_S3_BUCKET` | `arkloop` | |
| `ARKLOOP_S3_REGION` | `us-east-1` | |

### Authentication & Encryption

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_AUTH_JWT_SECRET` | — | Required |
| `ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS` | `900` | Access Token TTL |
| `ARKLOOP_AUTH_REFRESH_TOKEN_TTL_SECONDS` | `2592000` | Refresh Token TTL |
| `ARKLOOP_ENCRYPTION_KEY` | — | AES-256-GCM key (Required) |

### API

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_API_GO_ADDR` | `0.0.0.0:8001` | Listener address (inside container) |
| `ARKLOOP_API_PORT` | `8001` | Host port mapping used by `compose.dev.yaml` |
| `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` | — | Platform admin user_id (UUID) for initial deployment |

### Gateway

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_GATEWAY_UPSTREAM` | `http://api:8001` | Upstream API address |
| `ARKLOOP_GATEWAY_PORT` | `8000` | Default public entry point |
| `ARKLOOP_GATEWAY_TRUST_INCOMING_TRACE_ID` | `0` | Whether to trust upstream `X-Trace-Id` |
| `ARKLOOP_RATELIMIT_CAPACITY` | `60` | Rate limit bucket capacity |
| `ARKLOOP_RATELIMIT_RATE_PER_MINUTE` | `60` | Replenishment rate per minute |

### Worker

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_WORKER_CONCURRENCY` | `4` | Worker concurrency level |
| `ARKLOOP_WORKER_QUEUE_JOB_TYPES` | `run.execute` | Job types handled |

### Debugging

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_LLM_DEBUG_EVENTS` | `0` | Write raw LLM chunks to run_events |
| `ARKLOOP_TOOL_ALLOWLIST` | Empty | Deprecated compatibility flag; logged but does not gate runtime tools |
