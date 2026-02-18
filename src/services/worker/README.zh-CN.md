# Worker（Go）

这是 Arkloop 的 Go Worker，实现一条完整的执行闭环：
- 消费 Postgres `jobs` 队列中的 `run.execute`
- 执行原生 RunEngine（Provider 路由 + Agent Loop + Tools + Skills + MCP）
- 把事件写入 `run_events`（API 的 SSE 回放基于同一张表）

## 运行模式

- 未设置 `ARKLOOP_DATABASE_URL` / `DATABASE_URL`：只启动并等待退出（用于验证二进制与配置）。
- 设置了数据库连接串：进入消费模式，执行 native handler（默认）。

## 常用环境变量

- 数据库：
  - `ARKLOOP_DATABASE_URL` / `DATABASE_URL`
- Worker loop：
  - `ARKLOOP_WORKER_CONCURRENCY`（默认 4）
  - `ARKLOOP_WORKER_POLL_SECONDS`（默认 0.25）
  - `ARKLOOP_WORKER_LEASE_SECONDS`（默认 30）
  - `ARKLOOP_WORKER_HEARTBEAT_SECONDS`（默认 10；设为 0 可禁用 heartbeat）
  - `ARKLOOP_WORKER_QUEUE_JOB_TYPES`（默认 `run.execute`）
- Provider 路由：
  - `ARKLOOP_PROVIDER_ROUTING_JSON`（为空时默认走 stub）
- Tools：
  - `ARKLOOP_TOOL_ALLOWLIST`（为空时禁用全部工具）
- 调试：
  - `ARKLOOP_LLM_DEBUG_EVENTS=1`：把 `llm.request/llm.response.chunk` 写入 `run_events`
- MCP（可选）：
  - `ARKLOOP_MCP_CONFIG_FILE=./mcp.config.json`
- dotenv（可选）：
  - `ARKLOOP_LOAD_DOTENV=1`
  - `ARKLOOP_DOTENV_FILE=.env`（不设置时默认在仓库根目录找 `.env`）

## 本地测试

```bash
cd src/services/worker
go test ./...
```

## 多平台构建

```bash
cd src/services/worker
GOOS=linux GOARCH=amd64 go build ./cmd/worker
GOOS=darwin GOARCH=arm64 go build ./cmd/worker
GOOS=windows GOARCH=amd64 go build ./cmd/worker
```

## 本地运行（配合 API）

启动 Postgres（示例）：

```bash
docker compose up -d postgres
```

启动 API（示例）：

```bash
export ARKLOOP_LOAD_DOTENV=1
python -m alembic upgrade head
cd src/services/api && go run ./cmd/api
```

另开终端启动 Worker：

```bash
export ARKLOOP_LOAD_DOTENV=1
cd src/services/worker
go run ./cmd/worker
```
