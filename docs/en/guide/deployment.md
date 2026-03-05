# Deployment Guide

Arkloop orchestrates all services via `compose.yaml`, enabling a full deployment with a single command.

## Service Overview

| Service | Description | Default Port |
|------|------|---------|
| `postgres` | PostgreSQL 16 | 5432 |
| `pgbouncer` | Connection Pool | 5433 |
| `redis` | Cache/Queue | 6379 |
| `minio` | Object Storage | 9000 / 9001 |
| `migrate` | Database Migrations (One-time, exits after completion) | — |
| `api` | Control Plane API (Go) | 8001 |
| `gateway` | Reverse Proxy + Rate Limiting | 8000 |
| `worker` | Job Worker (Agent Execution Plane) | — |
| `sandbox` | Code Sandbox (Firecracker / Docker) | 8002 |
| `browser` | Browser Service (Playwright) | 3100 |
| `openviking` | Vector Memory Service | 1933 |

Startup order is guaranteed by `depends_on`: postgres → pgbouncer → migrate → api/worker → gateway, and redis → api/gateway/worker.

## Quick Start

### 1. Prepare Environment Variables

```bash
cp .env.example .env
```

Edit `.env` and set at least the following required fields:

| Variable | Description |
|------|------|
| `ARKLOOP_POSTGRES_PASSWORD` | PostgreSQL Password |
| `ARKLOOP_S3_SECRET_KEY` | MinIO/S3 Secret Key |
| `ARKLOOP_AUTH_JWT_SECRET` | JWT Signing Secret (at least 32 characters) |
| `ARKLOOP_ENCRYPTION_KEY` | AES-256-GCM Key (32-byte hex) |

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

The `migrate` service will automatically run migrations before `api/worker` starts and then exit. Check startup status:

```bash
docker compose ps
```

### 3. Access Services

| Endpoint | Description |
|------|------|
| `http://localhost:8000` | Public Entry point (with Gateway Rate Limiting/Auth) |
| `http://localhost:8001` | Direct API Access (for debugging) |
| `http://localhost:9001` | MinIO Console |

## Platform Administrator Initialization

On initial deployment, set the bootstrap platform administrator in `.env` (executed idempotently on API startup; this variable can be removed afterwards):

```bash
ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN=admin
```

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

### Docker Mode

macOS / Windows (WSL2) / No KVM environment, using Docker container isolation:

```bash
# Build sandbox-agent image
docker build -f src/services/sandbox/Dockerfile.agent -t arkloop/sandbox-agent:latest .

# Start
docker compose --profile docker-sandbox up -d sandbox-docker
```

In Docker mode, `sandbox-docker` uses host networking (agent container ports bind to 127.0.0.1 on the host).

### Local Development (Direct Run)

```bash
cd src/services/sandbox
go build -o sandbox-bin ./cmd/sandbox

# Docker Mode
ARKLOOP_SANDBOX_PROVIDER=docker \
ARKLOOP_SANDBOX_SOCKET_DIR=/tmp/sandbox \
ARKLOOP_SANDBOX_TEMPLATES_PATH="" \
./sandbox-bin
```

### Sandbox Configuration

Runtime parameters are configured via Console > Configuration > Sandbox page (written to `platform_settings` table), or can be overridden with environment variables:

| Config Key | Env Var | Default | Description |
|---|---|---|---|
| `sandbox.provider` | `ARKLOOP_SANDBOX_PROVIDER` | `firecracker` | Backend type |
| `sandbox.docker_image` | `ARKLOOP_SANDBOX_DOCKER_IMAGE` | `arkloop/sandbox-agent:latest` | Docker agent image |
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

## Local Development Mode

During development, you typically run the API on the host machine (for debugging/hot-reloading) while infrastructure runs in Docker:

```bash
# Start infrastructure only
docker compose up -d postgres redis minio pgbouncer

# Run migrations
cd src/services/api
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/migrate up

# Run API on host
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/api
```

If you need to use the Gateway, point upstream to the host:

```bash
ARKLOOP_GATEWAY_UPSTREAM=http://host.docker.internal:8001 docker compose up -d gateway
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

### Object Storage (MinIO/S3)

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_S3_ENDPOINT` | — | S3 endpoint URL |
| `ARKLOOP_S3_ACCESS_KEY` | `minioadmin` | |
| `ARKLOOP_S3_SECRET_KEY` | — | Required |
| `ARKLOOP_S3_BUCKET` | `arkloop` | |
| `ARKLOOP_S3_REGION` | `us-east-1` | |

### Authentication & Encryption

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_AUTH_JWT_SECRET` | — | Required |
| `ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS` | `900` | Access Token TTL |
| `ARKLOOP_AUTH_REFRESH_TOKEN_TTL_SECONDS` | `7776000` | Refresh Token TTL |
| `ARKLOOP_ENCRYPTION_KEY` | — | AES-256-GCM key (Required) |

### API

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_API_GO_ADDR` | `0.0.0.0:8001` | Listener address (inside container) |
| `ARKLOOP_API_PORT` | `8001` | Host port mapping |
| `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` | — | Platform admin login for initial deployment |

### Gateway

| Variable | Default | Description |
|------|--------|------|
| `ARKLOOP_GATEWAY_UPSTREAM` | `http://api:8001` | Upstream API address |
| `ARKLOOP_GATEWAY_PORT` | `8000` | Host port mapping |
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
| `ARKLOOP_TOOL_ALLOWLIST` | Empty | Allowed built-in tools for LLM; empty = disable all |
