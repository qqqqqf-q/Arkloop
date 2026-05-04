# Worker（Go）

这是 Arkloop 的 Go Worker，实现一条完整的执行闭环：
- 消费 Postgres `jobs` 队列中的 `run.execute`
- 执行原生 RunEngine（Provider 路由 + Agent Loop + Tools + Personas + MCP）
- 把事件写入 `run_events`（API 的 SSE 回放基于同一张表）

## 运行模式

- 未设置 `ARKLOOP_DATABASE_URL` / `DATABASE_URL`：只启动并等待退出（用于验证二进制与配置）。
- 设置了数据库连接串：进入消费模式，执行 native handler（默认）。

## 常用环境变量

- 数据库：
  - `ARKLOOP_DATABASE_URL` / `DATABASE_URL`
- Worker loop：
  - `ARKLOOP_WORKER_CONCURRENCY`（默认 4）
  - Desktop 侧car（SQLite 单进程）：`ARKLOOP_DESKTOP_WORKER_CONCURRENCY`（默认 2，上限 32；与 API 共用同一 `*sql.DB` 连接池）
  - `ARKLOOP_WORKER_POLL_SECONDS`（默认 0.25）
  - `ARKLOOP_WORKER_LEASE_SECONDS`（默认 30）
  - `ARKLOOP_WORKER_HEARTBEAT_SECONDS`（默认 10；设为 0 可禁用 heartbeat）
  - `ARKLOOP_WORKER_QUEUE_JOB_TYPES`（默认 `run.execute`）
- Provider 路由：
  - `ARKLOOP_PROVIDER_ROUTING_JSON`（为空时会选中 stub 路由；stub 网关默认关闭，需显式设置 `ARKLOOP_STUB_AGENT_ENABLED=true` 才会输出测试内容）
- Tools：
  - `ARKLOOP_TOOL_ALLOWLIST`（已弃用；仅记录日志，不再裁剪运行时工具）
  - `ARKLOOP_TOOL_PROVIDER_CACHE_TTL_SECONDS`（默认 `60`；0 表示不缓存，每次 run 都查 DB）
- 调试：
  - `ARKLOOP_LLM_DEBUG_EVENTS=1`：把 `llm.request/llm.response.chunk` 写入 `run_events`
- MCP（可选）：
  - `ARKLOOP_MCP_CONFIG_FILE=./mcp.config.json`
  - `ARKLOOP_MCP_CACHE_TTL_SECONDS`（默认 `60`；0 表示不缓存，每次 run 都查 DB）
- dotenv（可选）：
  - `ARKLOOP_LOAD_DOTENV=1`
  - `ARKLOOP_DOTENV_FILE=.env`（不设置时默认在仓库根目录找 `.env`）

## 本地测试

```bash
cd src/services/worker
go test -race ./...
```

### 核心路径测试覆盖

| 模块 | 测试文件 | 覆盖范围 |
|------|----------|----------|
| SSRF 拦截 | `web_fetch/url_policy_test.go` | scheme/hostname/localhost/私有IP/IPv6 全路径 |
| Pipeline 路由 | `pipeline/mw_routing_test.go` | stub gateway/静态路由回退/provider kind/gateway 构建/API key |
| Pipeline 配额 | `pipeline/mw_entitlement_test.go` | nil 透传/credits 耗尽/releaseSlot 回调 |
| Pipeline 输入加载 | `pipeline/mw_input_loader_test.go` | nil pool panic/消息限额/链式执行 |
| Entitlement 解析 | `shared/entitlement/resolve_test.go` | 缓存类型/月份边界/扣费策略/非数值回退/nil receiver |
| SSE 客户端 | `web/src/__tests__/sse.test.ts` | SSEApiError 构造/属性/类型守卫 |
| 事件处理 | `web/src/__tests__/runEventProcessing.test.ts` | 计数溢出/segment 过滤/角色过滤/空内容/类型校验 |

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
