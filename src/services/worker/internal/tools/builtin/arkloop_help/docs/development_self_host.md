# 开发与自托管（仓库工作流）

> **自托管状态**（README）：路径仍在发展，Alpha 阶段**不保证**完整可用；以下内容来自仓库 **CONTRIBUTING / CLAUDE / compose.yaml**，便于贡献者与进阶用户自查。

## 环境前置

- **Go**：1.26+（CLAUDE 与 CONTRIBUTING 约定）
- **Node**：20+，包管理 **pnpm**
- **Docker Compose**：用于本地 Postgres、Redis 等
- 复制 **`.env.example`** → **`.env`**，按说明填写

## 最小基础设施（Docker）

与 CLAUDE 一致的最小步：

```bash
docker compose up -d postgres redis
```

可选层（**以根目录 `compose.yaml` 的 `profiles` 名为准**）：

- **S3 兼容存储（SeaweedFS）**：`docker compose --profile s3 up -d seaweedfs`
- **PgBouncer**：`docker compose --profile pgbouncer up -d pgbouncer`（若文档写过 `performance` / `redis_gateway`，请打开 compose 对照实际服务名）
- **OpenViking**：`docker compose --profile openviking up -d openviking`（默认文档端口常见 **19010**）
- **Bridge**：`docker compose --profile bridge up -d bridge`

## 数据库迁移

**本地开发者（Go）**：

```bash
cd src/services/api && go run ./cmd/migrate
```

**Compose**：通常有 **`migrate` 服务**在 `api` / `worker` 之前执行 `up`；若以容器为主，请遵循 `compose.yaml` 中 `depends_on` 与健康检查。

## 单独启动各 Go 服务（开发）

```bash
cd src/services/api && go run ./cmd/api
cd src/services/gateway && go run ./cmd/gateway
cd src/services/worker && go run ./cmd/worker
```

CONTRIBUTING 中亦可能出现 `go run .` 等变体，**以各服务 `cmd/` 目录实际 `main` 为准**。

## 前端本地开发

```bash
pnpm install   # 仓库根目录
cd src/apps/web && pnpm dev
cd src/apps/console && pnpm dev
```

开发模式下常见 **Vite 代理 `/v1`** 到本机后端；Web 默认开发端口 **5173**，Compose 生产映射常见 **19080**（见 CLAUDE 前端表格）。

## 常用端口速查（文档约定）

| 用途 | 端口 |
|------|------|
| Gateway | 19000 |
| API | 19001 |
| Sandbox（容器内常见绑定，视 profile） | 19002 |
| Bridge | 19003 |
| Web（compose） | 19080 |
| Console | 19081 |
| Console-lite | 19082 |
| OpenViking（文档/compose 常见映射） | 19010 |

Postgres/Redis 在默认 `compose.yaml` 中**常不暴露宿主机端口**，仅在 Docker 网络内访问；若需本机直连，请自行在 compose 中增加 `ports` 或使用 `docker compose exec`。

## CI 本地脚本

```bash
bin/ci-local quick        # 快速检查
bin/ci-local integration  # Go 集成类
bin/ci-local full         # 全量
```

## 与 `docs/installation.md` 的关系

仓库另有面向安装场景的 **`docs/installation.md`**（含 `setup.sh doctor`、端口占用检查等），其中 **profile/场景命名**可能与 **`compose.yaml` 的 profile 字符串**不是同一套概念；**以当前 compose 与 .env 为准**，避免混用两套名称导致命令无法执行。

## 测试（Go）

```bash
cd src/services/api && go test ./...
cd src/services/worker && go test ./...
```

Desktop 相关测试常需 **`-tags desktop`**（见 worker `composition_desktop_test` 等）。
