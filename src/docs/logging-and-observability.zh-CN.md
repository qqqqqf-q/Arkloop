# 日志与可观测性（Logging/Observability）方案（草案）

本文目标：把 Arkloop 的日志系统提前定成“可落地、可审计、可演进”的形态，避免后期在多租户、流式、worker、以及工具审计落库之后再返工。

## 0. 默认策略（当前约定）

为了减少早期决策成本，同时避免后期返工，Arkloop 暂定以下默认值（后续只增不破）：

- **默认输出 JSON**：应用日志以 `stdout` 单行 JSON 为准，便于容器/CI/日志平台采集；线下机如需本地轮转，作为可选项再加。
- **默认服务端生成 `trace_id`**：普通客户端传入的 `trace_id` 不视为可信；仅在“受信任网关/上游”场景允许透传（否则会影响排障一致性与安全审计）。
- **Phase 1 不强依赖 OpenTelemetry**：先用 `trace_id` 把字段与链路贯穿跑通；OTel 作为 Phase 2+ 的增强接入，要求与日志字段对齐（`trace_id/span_id`）。

## 1. 三类“记录”的边界（先分清）

Arkloop 至少会同时存在三类记录，它们用途不同，不能混为一谈：

1) **Run Events（业务事件流）**：系统的唯一真相（SSE 推送 + 落库 + 回放）。用于“解释一次 run 的执行链路”，稳定且可测试。
2) **Application Logs（应用日志）**：运维与排障用途（服务健康、异常栈、耗时、依赖错误）。默认输出到 stdout，以结构化 JSON 为主。
3) **Security/Audit Logs（安全审计日志）**：后台管理动作、访问/导出、权限变更等。通常需要更严格的留存/不可篡改策略（阶段性落地）。

本方案讨论的是第 2 类：**Application Logs**，并要求它与 Run Events 的字段协同（trace_id/run_id 等）。

## 2. logger 放在哪里（代码归属建议）

推荐把“日志/trace/context 这套能力”做成共享包，供 `api` 与 `worker` 复用：

- `src/packages/observability/`
  - `context.py`：基于 `contextvars` 的上下文（`trace_id`、`org_id`、`run_id` 等）
  - `logging.py`：日志初始化与结构化输出（JSON）、脱敏、采样
  - `http.py`：FastAPI 中间件（注入/生成 `trace_id`、请求摘要、错误统一）
  - `worker.py`：任务上下文传播（从 job payload 恢复上下文）
  - `otel.py`（可选）：OpenTelemetry 接入（trace/span 与日志字段对齐）

原则：
- **`agent-core` 不直接依赖 logging**：核心逻辑应通过产出 Run Events 来表达过程；真正的日志落点在 `api/worker` 这类“边界层”。
- **模块自上而下注入**：由 `api`/`worker` 的 composition root 负责 `configure_logging()`，业务模块只拿到“已配置的 logger / context accessor”。

## 3. 最小必须能力（Phase 0/1）

### 3.1 结构化 JSON（默认 stdout）

目标是让日志天然可被采集、查询与聚合（Loki/ELK/Datadog 都吃得进去）。

建议日志输出满足：
- 单行 JSON（避免多行破坏采集）
- 时间戳统一为 ISO8601（UTC）或 epoch_ms（择一）
- 支持把 `exception`/`stack` 序列化成字段（生产可检索）

### 3.2 trace_id 全链路贯穿（强约束）

- HTTP 入口生成 `trace_id`（或从受信任上游透传），写入：
  - 日志字段：`trace_id`
  - API 错误响应：`trace_id`
  - HTTP Header：`X-Trace-Id`（便于前端/CLI 关联）
  - Run Events：同一条链路复用同一个 `trace_id`
- 进入 worker 时必须透传 `trace_id`（例如 job payload），并在 worker 侧恢复上下文。

建议额外区分：
- `request_id`：一次 HTTP 请求的 ID（可能一个 run 触发多个请求/回放）
- `run_id`：一次 Agent Loop 的执行实例

### 3.3 上下文自动注入（context binding）

日志不应靠“每行手写字段”，而是通过上下文绑定自动补齐：

必选上下文字段（优先级从高到低）：
- `trace_id`、`request_id`
- `org_id`、`user_id`
- `project_id`、`thread_id`、`run_id`
- `tool_call_id`、`event_id`（如果在工具执行/事件写入边界）
- `component`（`api`/`worker`/`agent-core`/`db` 等）

### 3.4 脱敏与“只记摘要”

企业系统最常见的事故之一就是“把敏感明文写进日志”。Arkloop 需要默认做到：

- 绝不记录：`Authorization`、`Cookie`、模型厂商 Key、System Prompt 原文
- 工具参数/输出：默认记录 `args_hash` / `result_hash`、`tool_name`、耗时、错误分类；明文参数应只进入 Run Events（且仍需按策略脱敏/分级）
- 对用户输入/模型输出：应用日志只记长度/摘要（或完全不记），回放与审计以 Run Events 为准

脱敏策略建议做成可配置“规则表”，而不是散落在各处的 `replace("***")`。

### 3.5 错误分类与可观测性字段

日志与 API 错误模型应对齐，最小字段建议包含：
- `error_class`（稳定分类，如 `auth.*`、`validation.*`、`policy.*`、`budget.*`、`provider.*`、`internal.*`）
- `message`（给人看的短句，避免泄露敏感细节）
- `retryable`（可选，便于告警与重试策略）
- `duration_ms`、`cost_usd`（可选，但建议尽早打通）

## 4. 推荐日志字段（建议固定 schema）

建议统一输出字段（可按实现细化，但字段名尽量稳定）：

- 通用：`ts`、`level`、`logger`、`msg`、`component`、`env`、`version`
- 关联：`trace_id`、`request_id`、`org_id`、`user_id`、`project_id`、`thread_id`、`run_id`
- 执行：`duration_ms`、`attempt`、`timeout_ms`
- 工具：`tool_name`、`tool_call_id`、`args_hash`、`result_hash`、`risk_level`
- 费用：`provider`、`model`、`input_tokens`、`output_tokens`、`cost_usd`
- 错误：`error_class`、`error_code`、`exception`（序列化）、`stack`（序列化）

说明：字段命名保持“蛇形 + 小写”，避免混用 `traceId/trace_id`。

## 5. 与 Run Events 的分工（避免重复造一套真相）

建议坚持一个简单规则：

- **业务过程**（谁调用了什么工具、参数/结果摘要、策略拦截、预算变化）写 Run Events，并落库可回放。
- **系统过程**（依赖异常、超时、数据库错误、worker 崩溃、上游不稳定）写应用日志，供运维排障与告警。

同一个事实如果两边都要有：
- Run Events 保留业务语义
- 应用日志只保留关联字段 + hash + 耗时，不重复写敏感明文

## 6. 实现选型建议（Python/FastAPI）

不追求“花哨”，追求“可组合与可演进”：

- 日志底座：Python `logging`
- 结构化与上下文绑定：优先 `structlog`（方便自动注入字段、JSON 输出、processor 链做脱敏）
- trace：Phase 0 先用自研 `trace_id`；Phase 1/2 再接 OpenTelemetry，把 `trace_id/span_id` 对齐到日志字段
- 异常聚合：可选 Sentry（或同类），但要明确“脱敏后上报”

## 7. 最小验收口径（建议写进 Phase 0）

Phase 0 验收建议至少包含：
- 任意 API 错误响应都有 `trace_id`
- `api` 与 `worker` 的日志都输出 JSON，并包含 `trace_id`
- 工具执行的日志不包含敏感明文（至少做到默认只记录 hash）
- pytest 可以断言：同一 run 的事件序列与日志里的 `trace_id/run_id` 可关联（不要求逐字一致）
