---
---

# Run 执行架构（API / Worker / 辅助服务）

本文描述 Arkloop 的 Run 执行拓扑与服务边界。核心设计：控制面与执行面彻底分离。

- **API**：鉴权、资源编排、审计落库、SSE 回放、enqueue job
- **Worker**：执行 Agent Loop、工具调用、事件写入
- **Gateway**：反向代理、速率限制、访问日志
- **Sandbox**：Firecracker 微虚拟机代码执行
- **Browser**：Playwright 浏览器自动化
- `run_events` 作为唯一真相

## 1. 当前拓扑

```
Client (Web/CLI) --HTTP+SSE--> API (Go, 控制面)
                                 |
                                 v
                        PostgreSQL (runs/run_events/messages/jobs/audit_logs)
                                 ^
                                 |
                        Worker (Go, 执行面) --lease jobs--> PostgreSQL
                                 |
                        +--------+--------+--------+
                        |        |        |        |
                     LLM API   MCP    Sandbox   Browser
                  (OpenAI/     Server  (Firecracker) (Playwright)
                   Anthropic)
```

关键约束：
- API 不执行 Agent Loop、不触发任何 tool executor
- Worker 是执行面唯一事实来源
- API 可扩容性来自它只做轻控制面（DB CRUD + SSE 回放）
- Redis 用于速率限制和组织级并发 run 控制

## 2. 核心不变量

- `run_events`：唯一真相（Worker 写入，API 读取并 SSE 回放）
- `jobs.payload_json`：跨服务协议（API 写，Worker 读），必须版本化
- `seq`：run 内单调递增，回放只用 `after_seq` 续传

## 3. 最小闭环链路

1) Client 创建 run：`POST /v1/threads/{id}/runs`

2) API（同一事务内）：
   - 写 `runs` 行
   - 写第一条事件 `run.started`
   - 插入 `jobs`（`run.execute`）

3) Worker 消费 `jobs`：
   - lease job（PostgreSQL Advisory Lock）
   - 执行 Pipeline（中间件链 -> Agent Loop）
   - 追加 `run_events`
   - 写 `messages`（assistant 归并结果）
   - 通过 PG `NOTIFY` 通知 API

4) Client 通过 SSE 回放：`GET /v1/runs/{id}`
   - `after_seq` 作为唯一游标
   - `follow=true` 时 API 发送心跳（15s），避免代理断链

## 4. Worker Pipeline 中间件

Worker 使用中间件链模式处理 run，顺序执行：

| 序号 | 中间件 | 职责 |
|------|--------|------|
| 1 | `mw_input_loader` | 加载 run 输入与线程消息 |
| 2 | `mw_cancel_guard` | 处理取消信号（提前失败） |
| 3 | `mw_entitlement` | 检查配额/功能权限（例如 runs/tokens quota） |
| 4 | `mw_mcp_discovery` | 发现 MCP 工具（org 级 + 缓存） |
| 5 | `mw_spawn_agent` | 按需注入 `spawn_agent` 工具（父子 run） |
| 6 | `mw_tool_provider` | 注入 Tool Provider（org 覆盖 platform）并绑定 executor |
| 7 | `mw_agent_config` | 组装 Persona 运行时配置（model selector、reasoning mode、prompt cache control） |
| 8 | `mw_persona_resolution` | 解析 persona_id、system prompt 与 persona 配置 |
| 9 | `mw_memory` | 注入长期记忆（可选） |
| 10 | `mw_routing` | LLM Provider 路由决策、构建 Gateway |
| 11 | `mw_title_summarizer` | 生成会话标题（可选） |
| 12 | `mw_tool_build` | 构建 ToolSpecs 与工具分发器（按 allowlist 过滤） |
| 13 | `handler_agent_loop` | 主 Agent Loop（LLM 调用 + 工具执行） |

### 4.1 中间件测试覆盖

核心中间件均有独立单元测试（`go test -race`）：

- `mw_input_loader`：nil pool panic 检测、消息限额回退、链式执行验证
- `mw_entitlement`：nil resolver 透传、credits 耗尽 panic 路径、releaseSlot 回调
- `mw_persona_resolution`：系统提示词分层、参数钳制、工具 allowlist/denylist 交集
- `mw_routing`：stub gateway、静态路由回退、provider kind 分发、API key 校验
- `mw_memory`：nil provider/nil userID no-op、注入/提交/错误降级

安全相关：`web_fetch/url_policy_test.go` 覆盖 SSRF 拦截全路径（scheme、hostname、localhost、私有 IP、IPv6）。

## 5. 执行器类型

Worker 通过 Executor Registry 支持多种执行器：

| 执行器 | 说明 |
|--------|------|
| `native_v1` | 标准 Agent（工具调用循环） |
| `agent.simple` | 基础 prompt-only 执行 |
| `task.classify_route` | 任务分类路由（判断 Pro/Ultra） |
| `agent.lua` | Lua 脚本 Agent（支持 `context.emit()` 自定义事件） |
| `agent.interactive` | Human-in-the-loop 模式 |
| `noop` | 空操作（测试用） |

## 6. Personas 体系

Personas 从 `src/personas/` 目录和数据库加载，每个 Persona 包含：
- `persona.yaml` -- 元数据与配置
- `prompt.md` -- System Prompt
- `agent.lua` -- Lua 脚本（可选）

当前内置 Personas：

| Persona | 执行器 | 说明 |
|-------|--------|------|
| `auto` | `task.classify_route` | 任务复杂度分类，路由到 Pro/Ultra |
| `lite` | `agent.simple` | 轻量快速响应 |
| `pro` | `agent.simple` | 通用能力 + 工具 |
| `search` | `agent.simple` | 搜索优化（含 `web_search` 工具） |
| `ultra` | `agent.simple` | 最大推理深度 |

Persona 配置字段：`id`、`executor_type`、`executor_config`、`tool_allowlist`、`tool_denylist`、`budgets`（reasoning_iterations、tool_continuation_budget、max_output_tokens、temperature、top_p、per_tool_soft_limits）。

## 7. 工具执行

### 7.1 内置工具

| 工具 | 说明 |
|------|------|
| `web_search` | 搜索（执行后端可由 Console 的 Tool Providers 配置覆盖） |
| `web_fetch` | 抓取（执行后端可由 Console 的 Tool Providers 配置覆盖） |
| `sandbox` | 代码执行（Firecracker 微虚拟机） |
| `browser` | 浏览器自动化（Playwright） |
| `spawn_agent` | 生成子 Agent run |
| `summarize_thread` | 会话摘要 |
| `echo` | 测试回声 |
| `noop` | 空操作 |

### 7.2 工具安全

- **Allowlist**：`ARKLOOP_TOOL_ALLOWLIST`（已弃用；仅为兼容保留日志，不再裁剪运行时工具）
- **Denylist**：Persona 级 `tool_denylist`
- LLM 只能看到白名单内的工具
- 每个工具执行有超时控制（`tool_timeout_ms`）

补充：`web_search` / `web_fetch` 是 Tool Group 名（LLM 只会看到 group）。Worker 运行时会解析到具体 Provider（例如 `web_search.tavily`、`web_fetch.jina`），解析优先级为：

1) org scope 激活的 provider（DB `tool_provider_configs.scope='org'`）  
2) platform scope 激活的 provider（DB `tool_provider_configs.scope='platform'`）  
3) legacy group executor（Config Resolver 的 `web_search.*` / `web_fetch.*`，支持 env override）  
4) 否则返回 `tool.not_configured`

说明：Tool Provider 的激活/凭证通过 Console 的 Tool Providers 配置页（`/v1/tool-providers`）管理。platform scope 用于全局默认；org scope 用于租户覆盖。

### 7.3 MCP 工具

- 支持 Stdio（子进程）和 HTTP 两种传输
- 每个 Org 独立配置 MCP 服务器（`mcp_configs` 表）
- 发现结果缓存（TTL：`ARKLOOP_MCP_CACHE_TTL_SECONDS`，默认 60s）
- 错误分类：`mcp.timeout`、`mcp.disconnected`、`mcp.rpc_error`、`mcp.protocol_error`、`mcp.tool_error`

## 8. Provider 路由

路由配置从数据库中的 Provider 账号与模型路由加载：
- 检查 run 请求中的 `route_id`
- 验证路由存在、凭证可访问、BYOK 是否启用
- 输出：`SelectedProviderRoute` 或 `ProviderRouteDenied`（`policy.route_not_found`、`policy.byok_disabled`）
- 无 DB 路由配置时，Worker 可回退到环境变量静态路由（开发/自托管场景）

支持的 LLM 提供商：
- OpenAI（及兼容 API）
- Anthropic
- Stub（测试用）

LLM 重试策略：429/502/503 自动重试，指数退避（默认 3 次，1s 基础延迟）。

## 9. 辅助服务

### 9.1 Gateway（`src/services/gateway/`）

HTTP 反向代理，位于 API 前端：
- 速率限制
- 访问日志
- 请求转发

### 9.2 Sandbox（`src/services/sandbox/`）

Firecracker 微虚拟机代码执行：
- Worker 通过 `ARKLOOP_SANDBOX_BASE_URL` 调用
- 隔离执行用户代码

### 9.3 Browser（`src/services/browser/`）

Node.js + Playwright 浏览器自动化：
- Worker 通过 `ARKLOOP_BROWSER_BASE_URL` 调用
- 用于 `web_fetch` 渲染和 `browser` 工具

### 9.4 OpenViking（外部服务）

用户记忆系统：
- Worker 通过 `ARKLOOP_OPENVIKING_BASE_URL` + `ARKLOOP_OPENVIKING_ROOT_API_KEY` 调用
- 加载/保存用户记忆快照
- 数据存储在 `user_memory_snapshots` 表

## 10. 任务队列

PostgreSQL 表实现的任务队列：

| 任务类型 | 说明 |
|----------|------|
| `run.execute` | 执行 Agent Loop |
| `webhook.deliver` | 投递 Webhook |
| `email.send` | 发送邮件 |

队列参数：
- 并发度：`ARKLOOP_WORKER_CONCURRENCY`（默认 4）
- 轮询间隔：`ARKLOOP_WORKER_POLL_SECONDS`（默认 0.25s）
- 租约时长：`ARKLOOP_WORKER_LEASE_SECONDS`（默认 30s）
- 心跳间隔：`ARKLOOP_WORKER_HEARTBEAT_SECONDS`（默认 10s）

Worker 启动时向 `worker_registrations` 表注册能力与版本。

## 11. 配置（Worker 相关 env）

| 变量 | 说明 |
|------|------|
| `ARKLOOP_DATABASE_URL` | PostgreSQL 连接 |
| `ARKLOOP_WORKER_CONCURRENCY` | 并发度（默认 4） |
| `ARKLOOP_WORKER_POLL_SECONDS` | 轮询间隔（默认 0.25） |
| `ARKLOOP_WORKER_LEASE_SECONDS` | 租约时长（默认 30） |
| `ARKLOOP_WORKER_HEARTBEAT_SECONDS` | 心跳间隔（默认 10） |
| `ARKLOOP_WORKER_QUEUE_JOB_TYPES` | 消费的任务类型 |
| `ARKLOOP_WORKER_CAPABILITIES` | Worker 能力标签 |
| `ARKLOOP_WORKER_VERSION` | Worker 版本 |
| `ARKLOOP_TOOL_ALLOWLIST` | 已弃用的兼容配置；不再裁剪运行时工具 |
| `ARKLOOP_LLM_RETRY_MAX_ATTEMPTS` | LLM 重试次数（默认 3） |
| `ARKLOOP_LLM_RETRY_BASE_DELAY_MS` | 重试基础延迟（默认 1000） |
| `ARKLOOP_MCP_CACHE_TTL_SECONDS` | MCP 发现缓存 TTL（默认 60） |
| `ARKLOOP_TOOL_PROVIDER_CACHE_TTL_SECONDS` | Tool Provider 缓存 TTL（默认 60） |
| `ARKLOOP_LLM_DEBUG_EVENTS` | 调试事件开关 |
| `ARKLOOP_SANDBOX_BASE_URL` | Sandbox 服务地址 |
| `ARKLOOP_BROWSER_BASE_URL` | Browser 服务地址 |
| `ARKLOOP_OPENVIKING_BASE_URL` | 记忆系统地址 |
| `ARKLOOP_OPENVIKING_ROOT_API_KEY` | 记忆系统密钥 |
| `ARKLOOP_ENCRYPTION_KEY` | 凭证解密密钥 |

## 12. 常见问题（排障视角）

- **"run 一直 running"**：检查 `jobs` 是否被 Worker lease、`run_events` 是否有后续事件写入、Worker 心跳是否正常。
- **"SSE 偶发卡住"**：检查代理是否缓冲（API 应设置 `Cache-Control: no-cache`、`X-Accel-Buffering: no`）以及心跳。
- **"事件丢失/乱序"**：同一 run 内 `seq` 必须严格递增；回放只用 `after_seq` 续传。
- **"工具无响应"**：检查辅助服务可达性与运行时注册结果；`ARKLOOP_TOOL_ALLOWLIST` 已不再控制工具开关。
- **"MCP 工具超时"**：检查 `mcp_configs` 配置、MCP 服务器进程状态、缓存 TTL。
