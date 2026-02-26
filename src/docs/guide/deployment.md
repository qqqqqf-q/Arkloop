# 部署指南

Arkloop 通过 `compose.yaml` 编排所有服务，一条命令完成完整部署。

## 服务总览

| 服务 | 说明 | 默认端口 |
|------|------|---------|
| `postgres` | PostgreSQL 16 | 5432 |
| `pgbouncer` | 连接池 | 5433 |
| `redis` | 缓存/队列 | 6379 |
| `minio` | 对象存储 | 9000 / 9001 |
| `migrate` | 数据库迁移（一次性，完成后退出） | — |
| `api` | 控制面 API（Go） | 8001 |
| `gateway` | 反向代理 + 限流 | 8000 |
| `worker` | Job Worker（Agent 执行面） | — |

启动顺序由 `depends_on` 保证：postgres → pgbouncer → migrate → api → gateway，redis → gateway/worker。

## 快速开始

### 1. 准备环境变量

```bash
cp .env.example .env
```

编辑 `.env`，至少设置以下必填项：

| 变量 | 说明 |
|------|------|
| `ARKLOOP_POSTGRES_PASSWORD` | PostgreSQL 密码 |
| `ARKLOOP_S3_SECRET_KEY` | MinIO/S3 Secret Key |
| `ARKLOOP_AUTH_JWT_SECRET` | JWT 签名密钥（至少 32 字符） |
| `ARKLOOP_ENCRYPTION_KEY` | AES-256-GCM 密钥（32 字节 hex） |

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

`migrate` 服务会自动在 `api` 启动之前运行迁移并退出。查看启动状态：

```bash
docker compose ps
```

### 3. 访问服务

| 端点 | 说明 |
|------|------|
| `http://localhost:8000` | 对外入口（经过 Gateway 限流/鉴权） |
| `http://localhost:8001` | API 直连（调试用） |
| `http://localhost:9001` | MinIO Console |

## 平台管理员初始化

首次部署时，在 `.env` 中设置 bootstrap 管理员（API 启动时幂等执行，之后可移除此变量）：

```bash
ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN=admin
```

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

## 本地开发模式

开发时通常希望在宿主机运行 API（便于调试/热更新），只用 Docker 跑基础设施：

```bash
# 只起基础设施
docker compose up -d postgres redis minio pgbouncer

# 运行迁移
cd src/services/api
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/migrate up

# 在宿主机运行 API
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/api
```

此时如需使用 Gateway，覆盖 upstream 指向宿主机：

```bash
ARKLOOP_GATEWAY_UPSTREAM=http://host.docker.internal:8001 docker compose up -d gateway
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

### 对象存储（MinIO/S3）

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_S3_ENDPOINT` | — | S3 端点 URL |
| `ARKLOOP_S3_ACCESS_KEY` | `minioadmin` | |
| `ARKLOOP_S3_SECRET_KEY` | — | 必填 |
| `ARKLOOP_S3_BUCKET` | `arkloop` | |
| `ARKLOOP_S3_REGION` | `us-east-1` | |

### 鉴权与加密

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_AUTH_JWT_SECRET` | — | 必填 |
| `ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS` | `900` | Access Token 有效期 |
| `ARKLOOP_AUTH_REFRESH_TOKEN_TTL_SECONDS` | `7776000` | Refresh Token 有效期 |
| `ARKLOOP_ENCRYPTION_KEY` | — | AES-256-GCM 密钥（必填） |

### API

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_API_GO_ADDR` | `0.0.0.0:8001` | 监听地址（容器内） |
| `ARKLOOP_API_PORT` | `8001` | 宿主机映射端口 |
| `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` | — | 首次部署管理员 login |

### Gateway

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_GATEWAY_UPSTREAM` | `http://api:8001` | 上游 API 地址 |
| `ARKLOOP_GATEWAY_PORT` | `8000` | 宿主机映射端口 |
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
| `ARKLOOP_TOOL_ALLOWLIST` | 空 | 允许 LLM 调用的内置工具，空 = 禁用全部 |
