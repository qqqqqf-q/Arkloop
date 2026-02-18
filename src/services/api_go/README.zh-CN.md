# Go API（控制面）

这是 Arkloop 的 Go API，实现控制面（Auth/Threads/Messages/Runs/SSE 回放），用于在迁移期逐步替换 Python API：`src/services/api/`。

迁移期推荐并行运行：
- Python API：`127.0.0.1:8000`
- Go API：`127.0.0.1:8001`（默认）

## 本地运行

准备 Postgres：

```bash
docker compose up -d postgres
```

准备数据库迁移（仍由 Alembic 驱动）：

```bash
python -m alembic upgrade head
```

启动 Go API：

```bash
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_DOTENV_FILE=.env.test
cd src/services/api_go
go run ./cmd/api
```

默认监听 `127.0.0.1:8001`，可通过以下方式覆盖：
- `ARKLOOP_API_GO_ADDR=127.0.0.1:8001`
- 或 `PORT=8001`（部署环境常用）

## 常用环境变量

- 数据库：`ARKLOOP_DATABASE_URL` / `DATABASE_URL`
- 鉴权：`ARKLOOP_AUTH_JWT_SECRET`、`ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS`
- SSE：`ARKLOOP_SSE_POLL_SECONDS`、`ARKLOOP_SSE_HEARTBEAT_SECONDS`、`ARKLOOP_SSE_BATCH_LIMIT`
- trace：`ARKLOOP_TRUST_INCOMING_TRACE_ID=1`（仅在反向代理已生成 trace_id 时启用）

## P.09 切流（客户端切换）

- Web（本地开发）：`pnpm -C src/apps/web dev -- --mode api_go`
- CLI：`PYTHONPATH=src python -m arkloop chat --profile api_go --message "hello"`

回滚只涉及路由/配置：切回 `api_py` 即可，不需要 DB 回滚。
