---
---

# 部署指南

Arkloop 通过 `compose.yaml` 编排所有服务，一条命令完成完整部署。

## 服务总览

| 服务 | 说明 | 默认端口 |
|------|------|---------|
| `postgres` | PostgreSQL 16 | 5432 |
| `pgbouncer` | 连接池 | 5433 |
| `redis` | 缓存/队列 | 6379 |
| `seaweedfs` | S3 兼容对象存储 | 9000 |
| `migrate` | 数据库迁移（一次性，完成后退出） | — |
| `api` | 控制面 API（Go） | 8001 |
| `gateway` | 反向代理 + 限流 | 8000 |
| `worker` | Job Worker（Agent 执行面） | — |
| `sandbox` | 代码沙箱（Firecracker / Docker） | 8002 |
| `openviking` | 向量记忆服务 | 1933 |

启动顺序由 `depends_on` 保证：postgres → pgbouncer → migrate → api/worker → gateway，redis → api/gateway/worker，seaweedfs → api/worker/sandbox。

## 快速开始

### 1. 准备环境变量

```bash
cp .env.example .env
```

编辑 `.env`，至少设置以下必填项：

| 变量 | 说明 |
|------|------|
| `ARKLOOP_POSTGRES_PASSWORD` | PostgreSQL 密码 |
| `ARKLOOP_AUTH_JWT_SECRET` | JWT 签名密钥（至少 32 字符） |
| `ARKLOOP_ENCRYPTION_KEY` | AES-256-GCM 密钥（32 字节 hex） |
| `ARKLOOP_STORAGE_BACKEND` | 存储后端，默认 `filesystem` |
| `ARKLOOP_STORAGE_ROOT` | 本地存储根目录 |

生成安全密钥：

```bash
# JWT Secret（至少 32 字符）
openssl rand -base64 48

# Encryption Key（32 字节 hex）
openssl rand -hex 32
```

### 2. 启动全部服务

```bash
docker compose up -d
```

默认 compose 使用本地 `filesystem` 存储，适合单机/单节点部署。若切换到 SeaweedFS / S3 兼容对象存储，请显式设置 `ARKLOOP_STORAGE_BACKEND=s3`，并补齐 `ARKLOOP_S3_*` 配置。

`migrate` 服务会自动在 `api/worker` 启动之前运行迁移并退出。查看启动状态：

```bash
docker compose ps
```

### 3. 访问服务

| 端点 | 说明 |
|------|------|
| `http://localhost:8000` | 对外入口（经过 Gateway 限流/鉴权） |

默认情况下，内部服务只在 Docker 网络内暴露。如果需要宿主机调试端口，请显式叠加开发覆盖文件：

```bash
docker compose -f compose.yaml -f compose.dev.yaml up -d
```

## 平台管理员初始化

首次部署时，可以通过环境变量把指定用户提升为 `platform_admin`（一次性执行）。

步骤：
1. 先注册/登录一个账号
2. 调用 `GET /v1/auth/me` 获取 `id`
3. 在 `.env` 中设置：

```bash
ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN=<user_id>
```

然后重启 `api` 服务。

## Tool Providers（可选）

`web_search` / `web_fetch` 等工具需要配置后端 Provider 与凭证。

推荐方式（SaaS / 自托管通用）：
- 用 bootstrap 的 `platform_admin` 登录 Console
- 在 Tool Providers 中使用 `scope=platform` 配置一次作为全局默认
- 如需单个租户自定义，再用 `scope=org` 覆盖

兼容方式（仅用于本地快速跑通）：
- 直接用环境变量配置 legacy `web_search.*` / `web_fetch.*`（例如 `ARKLOOP_WEB_SEARCH_PROVIDER`、`ARKLOOP_WEB_SEARCH_TAVILY_API_KEY`）

## 查看日志

```bash
# 所有服务
docker compose logs -f

# 单个服务
docker compose logs -f api
docker compose logs -f worker
docker compose logs -f gateway
```

## 重新部署（更新代码后）

```bash
docker compose build api worker gateway
docker compose up -d
```

迁移会在 `api` 启动前自动重新执行。若需强制重跑迁移：

```bash
docker compose run --rm migrate up
```

## 停止 / 清理

```bash
# 停止，保留数据
docker compose down

# 停止并清除数据卷（重置数据库）
docker compose down -v
```

## Sandbox rootfs 构建

Sandbox 服务使用 Firecracker microVM 执行用户代码，需要预构建 rootfs ext4 镜像。

### 构建 rootfs

```bash
cd src/services/sandbox/rootfs
./build.sh
```

构建产物输出到 `src/services/sandbox/rootfs/output/python3.12.ext4`。

### 部署到目标机器

```bash
DEPLOY_HOST=user@host ./build.sh
```

默认部署路径为 `/opt/sandbox/rootfs/`，可通过 `DEPLOY_PATH` 覆盖。

### 预装环境

| 类别 | 内容 |
|------|------|
| Python 3.12 | numpy, pandas, matplotlib, scipy, sympy, pillow, scikit-learn, requests, httpx, beautifulsoup4, lxml, openpyxl, pyyaml, rich |
| Node.js 20 | node, npm, npx |
| 系统工具 | busybox, curl, git, jq, sqlite3 |

修改预装内容：编辑 `src/services/sandbox/rootfs/Dockerfile.python3.12` 后重新构建。

## Sandbox 部署

Sandbox 支持两种后端 Provider，通过 `sandbox.provider` 配置项（或 `ARKLOOP_SANDBOX_PROVIDER` 环境变量）切换。

### Firecracker 模式（默认）

Linux + KVM 环境，使用 microVM 隔离：

```bash
docker compose --profile firecracker up -d sandbox
```

需要 `/dev/kvm` 设备和 Firecracker 二进制。

推荐保持最小权限运行，不再要求 `privileged`。

### Docker 模式

macOS / Windows (WSL2) / 无 KVM 环境，使用 Docker 容器隔离：

```bash
# 指定用户态 Docker socket
export ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH=/run/user/1000/docker.sock

# 启动
docker compose --profile docker-sandbox up -d sandbox-docker
```

Compose 默认会使用 `src/services/sandbox/Dockerfile.agent` 构建本地 `sandbox-agent` 镜像，并打上 `arkloop/sandbox-agent:dev` 标签。

Linux 推荐使用 rootless Docker 的用户态 socket。
macOS / Windows Docker Desktop 请改为各自用户目录下的 socket 路径，不再使用系统级 `/var/run/docker.sock`。

`sandbox-docker` 自身运行在 Compose 网络中，派生出的 `sandbox-agent` 容器按策略接入专用网络，不使用 host 网络。启用外网时使用 `arkloop_sandbox_agent_egress`，禁用外网时使用 `arkloop_sandbox_agent_internal`；如需切换，可设置 `ARKLOOP_SANDBOX_ALLOW_EGRESS` 并重建网络。

Docker backend 镜像内置常用开发命令，包括 `git`、`curl`、`wget`、`jq`、`grep`、`rg`、`find`、`tar`、`zip`、`unzip`、`sqlite3`、`ssh`、`make` 等，保证终端型任务和仓库分析任务可直接运行。

### 本地开发（直接运行）

```bash
cd src/services/sandbox
go build -o sandbox-bin ./cmd/sandbox

# Docker 模式
ARKLOOP_SANDBOX_PROVIDER=docker \
DOCKER_HOST=unix:///run/user/1000/docker.sock \
ARKLOOP_SANDBOX_SOCKET_DIR=/tmp/sandbox \
ARKLOOP_SANDBOX_TEMPLATES_PATH="" \
./sandbox-bin
```

### Sandbox 配置

运行时参数通过 Console > Configuration > Sandbox 页面配置（写入 `platform_settings` 表），也可用环境变量覆盖：

| 配置项 Key | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `sandbox.provider` | `ARKLOOP_SANDBOX_PROVIDER` | `firecracker` | 后端类型 |
| `sandbox.allow_egress` | `ARKLOOP_SANDBOX_ALLOW_EGRESS` | `true` | Sandbox backend 是否允许访问外网 |
| `sandbox.docker_image` | `ARKLOOP_SANDBOX_DOCKER_IMAGE` | `arkloop/sandbox-agent:dev` | 本地 Docker backend 默认使用的 agent 镜像 |
| `sandbox.max_sessions` | `ARKLOOP_SANDBOX_MAX_SESSIONS` | `50` | 最大并发 session |
| `sandbox.boot_timeout_s` | `ARKLOOP_SANDBOX_BOOT_TIMEOUT_S` | `30` | 启动超时（秒） |
| `sandbox.warm_lite` | `ARKLOOP_SANDBOX_WARM_LITE` | `3` | lite 预热数 |
| `sandbox.warm_pro` | `ARKLOOP_SANDBOX_WARM_PRO` | `2` | pro 预热数 |
| `sandbox.warm_ultra` | `ARKLOOP_SANDBOX_WARM_ULTRA` | `1` | ultra 预热数 |
| `sandbox.idle_timeout_lite_s` | `ARKLOOP_SANDBOX_IDLE_TIMEOUT_LITE_S` | `180` | lite 空闲超时（秒） |
| `sandbox.idle_timeout_pro_s` | `ARKLOOP_SANDBOX_IDLE_TIMEOUT_PRO_S` | `300` | pro 空闲超时（秒） |
| `sandbox.idle_timeout_ultra_s` | `ARKLOOP_SANDBOX_IDLE_TIMEOUT_ULTRA_S` | `600` | ultra 空闲超时（秒） |
| `sandbox.max_lifetime_s` | `ARKLOOP_SANDBOX_MAX_LIFETIME_S` | `1800` | 最大存活时间（秒） |

部署级参数（仅 ENV，不进 Console）：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_SANDBOX_ADDR` | `0.0.0.0:8002` | 服务监听地址 |
| `ARKLOOP_FIRECRACKER_BIN` | `/usr/bin/firecracker` | Firecracker 二进制路径 |
| `ARKLOOP_SANDBOX_KERNEL_IMAGE` | `/opt/sandbox/vmlinux` | 内核镜像路径 |
| `ARKLOOP_SANDBOX_ROOTFS` | `/opt/sandbox/rootfs.ext4` | rootfs 路径 |
| `ARKLOOP_SANDBOX_SOCKET_DIR` | `/run/sandbox` | 临时文件目录 |
| `ARKLOOP_SANDBOX_TEMPLATES_PATH` | `/opt/sandbox/templates.json` | 模板文件路径 |
| `ARKLOOP_SANDBOX_EGRESS_INTERFACE` | `eth0` | Firecracker NAT 出口网卡 |
| `ARKLOOP_SANDBOX_FIRECRACKER_TAP_PREFIX` | `arktap` | Firecracker TAP 前缀 |
| `ARKLOOP_SANDBOX_FIRECRACKER_TAP_CIDR` | `172.29.0.0/16` | Firecracker TAP 地址池 |
| `ARKLOOP_SANDBOX_FIRECRACKER_DNS` | `1.1.1.1,8.8.8.8` | Firecracker guest DNS 列表 |
| `ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH` | - | `docker-sandbox` profile 必填，宿主机用户态 Docker socket 路径 |

## 本地开发模式

开发时通常希望在宿主机运行 API（便于调试/热更新），只用 Docker 跑基础设施：

```bash
# 只起基础设施
docker compose -f compose.yaml -f compose.dev.yaml up -d postgres redis seaweedfs pgbouncer

# 运行迁移
cd src/services/api
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/migrate up

# 在宿主机运行 API
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/api
```

此时如需使用 Gateway，覆盖 upstream 指向宿主机：

```bash
ARKLOOP_GATEWAY_UPSTREAM=http://host.docker.internal:8001 docker compose -f compose.yaml -f compose.dev.yaml up -d gateway
```

## 完整环境变量参考

### 数据库

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_DATABASE_URL` | — | 主连接串（生产环境指向 PgBouncer） |
| `ARKLOOP_DATABASE_DIRECT_URL` | — | 直连 DSN（SSE LISTEN/NOTIFY 专用） |
| `ARKLOOP_POSTGRES_USER` | `arkloop` | |
| `ARKLOOP_POSTGRES_PASSWORD` | — | 必填 |
| `ARKLOOP_POSTGRES_DB` | `arkloop` | |
| `ARKLOOP_PGBOUNCER_POOL_SIZE` | `200` | PgBouncer 连接池大小 |
| `ARKLOOP_PGBOUNCER_MAX_CLIENT_CONN` | `1000` | PgBouncer 最大客户端连接数 |

### Redis

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_REDIS_URL` | — | Redis 连接串 |
| `ARKLOOP_REDIS_PASSWORD` | `arkloop_redis` | |

### 存储

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_STORAGE_BACKEND` | `filesystem` | 本地部署默认值；多节点建议改为 `s3` |
| `ARKLOOP_STORAGE_ROOT` | `/var/lib/arkloop/storage` | `filesystem` 后端根目录 |

### 对象存储（可选 SeaweedFS / S3 兼容）

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_S3_ENDPOINT` | — | `s3` 后端的端点 URL |
| `ARKLOOP_S3_ACCESS_KEY` | `arkloop` | |
| `ARKLOOP_S3_SECRET_KEY` | — | `s3` 后端必填 |
| `ARKLOOP_S3_BUCKET` | `arkloop` | |
| `ARKLOOP_S3_REGION` | `us-east-1` | |

### 鉴权与加密

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_AUTH_JWT_SECRET` | — | 必填 |
| `ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS` | `900` | Access Token 有效期 |
| `ARKLOOP_AUTH_REFRESH_TOKEN_TTL_SECONDS` | `2592000` | Refresh Token 有效期 |
| `ARKLOOP_ENCRYPTION_KEY` | — | AES-256-GCM 密钥（必填） |

### API

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_API_GO_ADDR` | `0.0.0.0:8001` | 监听地址（容器内） |
| `ARKLOOP_API_PORT` | `8001` | `compose.dev.yaml` 使用的宿主机映射端口 |
| `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` | — | 首次部署管理员 user_id（UUID） |

### Gateway

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_GATEWAY_UPSTREAM` | `http://api:8001` | 上游 API 地址 |
| `ARKLOOP_GATEWAY_PORT` | `8000` | 默认对外入口 |
| `ARKLOOP_GATEWAY_TRUST_INCOMING_TRACE_ID` | `0` | 是否信任上游传入的 `X-Trace-Id` |
| `ARKLOOP_RATELIMIT_CAPACITY` | `60` | 限流桶容量 |
| `ARKLOOP_RATELIMIT_RATE_PER_MINUTE` | `60` | 每分钟补充速率 |

### Worker

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_WORKER_CONCURRENCY` | `4` | Worker 并发数 |
| `ARKLOOP_WORKER_QUEUE_JOB_TYPES` | `run.execute` | 处理的 Job 类型 |

### 调试

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_LLM_DEBUG_EVENTS` | `0` | LLM 原始 chunk 写入 run_events |
| `ARKLOOP_TOOL_ALLOWLIST` | 空 | 已弃用的兼容配置；仅记录日志，不再裁剪运行时工具 |
