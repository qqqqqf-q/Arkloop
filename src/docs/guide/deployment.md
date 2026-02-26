# 部署指南

Arkloop 由以下服务组成，均通过 `compose.yaml` 统一编排：

| 服务 | 说明 | 默认端口 |
|------|------|---------|
| `postgres` | PostgreSQL 16 | 5432 |
| `pgbouncer` | 连接池 | 5433 |
| `redis` | 缓存/队列 | 6379 |
| `minio` | 对象存储 | 9000 |
| `gateway` | 反向代理 + 限流 | 8000 |
| `worker` | Job Worker（Agent 执行面） | — |

API 服务（Go）目前在容器外运行，或单独部署。

## 1. 准备环境变量

```bash
cp .env.example .env
```

必须修改的变量：

| 变量 | 说明 |
|------|------|
| `ARKLOOP_POSTGRES_PASSWORD` | PostgreSQL 密码 |
| `ARKLOOP_S3_SECRET_KEY` | MinIO/S3 Secret Key |
| `ARKLOOP_AUTH_JWT_SECRET` | JWT 签名密钥（至少 32 字符） |
| `ARKLOOP_ENCRYPTION_KEY` | AES-256-GCM 加密密钥（32 字节 hex） |

生成安全密钥：

```bash
# JWT Secret
openssl rand -base64 48

# Encryption Key（32 字节 hex）
openssl rand -hex 32
```

## 2. 启动基础设施

```bash
docker compose up -d postgres redis minio pgbouncer
```

等待健康检查通过（所有服务 healthy）：

```bash
docker compose ps
```

## 3. 运行数据库迁移

```bash
cd src/services/api
ARKLOOP_DATABASE_URL="postgresql://arkloop:<password>@127.0.0.1:5432/arkloop" \
  go run ./cmd/api migrate up
```

或通过环境变量文件：

```bash
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/api migrate up
```

## 4. 启动 Gateway

```bash
docker compose up -d gateway
```

Gateway 通过 `ARKLOOP_GATEWAY_UPSTREAM` 代理到 API 服务，默认 `http://host.docker.internal:8001`。

## 5. 启动 Worker

```bash
docker compose up -d worker
```

或本地运行：

```bash
cd src/services/worker
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/worker
```

## 6. 启动 API 服务

```bash
cd src/services/api
ARKLOOP_LOAD_DOTENV=1 ARKLOOP_DOTENV_FILE=../../.env go run ./cmd/api
```

## 7. 构建前端

```bash
# Web
cd src/apps/web && pnpm install && pnpm build

# Console
cd src/apps/console && pnpm install && pnpm build
```

## 平台管理员初始化

首次部署时，在 `.env` 中配置 bootstrap 管理员（启动后可移除该变量）：

```bash
ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN=admin
```

API 启动时会幂等地将该用户提升为 `platform_admin` 角色。

## 完整环境变量参考

### 数据库

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_DATABASE_URL` | — | 主连接串（生产环境指向 PgBouncer） |
| `ARKLOOP_DATABASE_DIRECT_URL` | — | 直连 DSN（SSE LISTEN/NOTIFY 专用，不经过 PgBouncer） |
| `ARKLOOP_POSTGRES_USER` | `arkloop` | PostgreSQL 用户 |
| `ARKLOOP_POSTGRES_PASSWORD` | — | PostgreSQL 密码（必填） |
| `ARKLOOP_POSTGRES_DB` | `arkloop` | 数据库名 |

### Redis

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_REDIS_URL` | — | Redis 连接串 |
| `ARKLOOP_REDIS_PASSWORD` | `arkloop_redis` | Redis 密码 |

### 对象存储（MinIO/S3）

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_S3_ENDPOINT` | — | S3 端点 URL |
| `ARKLOOP_S3_ACCESS_KEY` | `minioadmin` | Access Key |
| `ARKLOOP_S3_SECRET_KEY` | — | Secret Key（必填） |
| `ARKLOOP_S3_BUCKET` | `arkloop` | Bucket 名 |
| `ARKLOOP_S3_REGION` | `us-east-1` | Region |

### 鉴权与加密

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_AUTH_JWT_SECRET` | — | JWT 签名密钥（必填） |
| `ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS` | `900` | Access Token 有效期（秒） |
| `ARKLOOP_AUTH_REFRESH_TOKEN_TTL_SECONDS` | `7776000` | Refresh Token 有效期（秒） |
| `ARKLOOP_ENCRYPTION_KEY` | — | AES-256-GCM 密钥（必填） |

### Gateway

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ARKLOOP_GATEWAY_ADDR` | `0.0.0.0:8000` | Gateway 监听地址 |
| `ARKLOOP_GATEWAY_UPSTREAM` | `http://host.docker.internal:8001` | 上游 API 地址 |
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
| `ARKLOOP_LLM_DEBUG_EVENTS` | `0` | 将 LLM 原始 chunk 写入 run_events（仅本地开发） |
| `ARKLOOP_TOOL_ALLOWLIST` | 空 | 允许 LLM 调用的内置工具（逗号分隔），空 = 禁用全部 |
