# 本地启动

## 依赖

- Go 1.22+
- Node.js 20+
- pnpm
- Docker（运行 PostgreSQL）

## 1. 启动 PostgreSQL

```bash
cp .env.example .env
# 编辑 .env，设置 ARKLOOP_POSTGRES_PASSWORD
docker compose up -d
```

连通性验证：

```bash
docker compose exec -T postgres psql -U arkloop -d arkloop -c "select 1;"
```

## 2. 启动 API 服务

默认监听 `127.0.0.1:8001`。

::: code-group

```bash [Linux/macOS]
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_DOTENV_FILE=.env
cd src/services/api && go run ./cmd/api
```

```powershell [Windows]
$env:ARKLOOP_LOAD_DOTENV="1"
$env:ARKLOOP_DOTENV_FILE=".env"
cd src/services/api; go run ./cmd/api
```

:::

覆盖监听地址：

```
ARKLOOP_API_GO_ADDR=127.0.0.1:8001
```

## 3. 启动 Worker 服务

```bash
cd src/services/worker && go run ./cmd/worker
```

## 4. 启动前端（Web）

```bash
cd src/apps/web && pnpm install && pnpm dev
```

## 5. 启动前端（Console）

```bash
cd src/apps/console && pnpm install && pnpm dev
```

## 集成测试

```bash
cd src/services/api && go test -tags integration ./...
```

## 环境变量速查

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `ARKLOOP_DATABASE_URL` | PostgreSQL 连接串 | — |
| `ARKLOOP_API_GO_ADDR` | API 监听地址 | `127.0.0.1:8001` |
| `ARKLOOP_LOAD_DOTENV` | 自动从 .env 文件加载 | `0` |
| `ARKLOOP_DOTENV_FILE` | .env 文件路径 | `.env` |
| `ARKLOOP_TOOL_ALLOWLIST` | 允许 LLM 调用的内置工具（逗号分隔） | 空（禁用全部） |
| `ARKLOOP_JWT_SECRET` | JWT 签名密钥 | — |

完整环境变量参考：`.env.example`。
