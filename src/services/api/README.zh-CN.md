# API（控制面 / Go）

这是 Arkloop 的 API 服务（控制面），负责：
- Auth/Threads/Messages/Runs 的 HTTP 接口
- SSE 回放（从 `run_events` 读取）
- enqueue `run.execute` job（由 Go Worker 执行）

说明：Python API 与 `in_process` 执行模式已下线；不再支持 `ARKLOOP_RUN_EXECUTOR`。

## 本地运行

准备 Postgres：

```bash
docker compose up -d postgres
```

准备数据库迁移（goose / Go）：

```bash
cd src/services/api
go run ./cmd/migrate up
```

其他迁移命令：

```bash
go run ./cmd/migrate status    # 查看当前版本与期望版本
go run ./cmd/migrate down      # 回滚一个版本
```

启动 Go API：

```bash
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_DOTENV_FILE=.env.test
cd src/services/api
go run ./cmd/api
```

默认监听 `127.0.0.1:19001`，可通过以下方式覆盖：
- `ARKLOOP_API_GO_ADDR=127.0.0.1:19001`
- 或 `PORT=19001`（部署环境常用）

## 常用环境变量

- 数据库：`ARKLOOP_DATABASE_URL` / `DATABASE_URL`
- 鉴权：`ARKLOOP_AUTH_JWT_SECRET`、`ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS`
- SSE：`ARKLOOP_SSE_POLL_SECONDS`、`ARKLOOP_SSE_HEARTBEAT_SECONDS`、`ARKLOOP_SSE_BATCH_LIMIT`
- trace：`ARKLOOP_TRUST_INCOMING_TRACE_ID=1`（仅在反向代理已生成 trace_id 时启用）

## 前端与 CLI

- Web：Vite dev proxy 默认走 `http://127.0.0.1:19001`；如需覆盖可设置 `ARKLOOP_API_PROXY_TARGET`
- CLI：`PYTHONPATH=. python -m tools.cli.cli chat --profile api --message "hello"`（参考客户端，位于 `tools/cli/`）
