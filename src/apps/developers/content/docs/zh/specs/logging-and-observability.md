---
title: "日志与可观测性方案"
---
本文描述 Arkloop 的日志系统架构，覆盖应用日志、审计日志与 Run Events 的分工。

## 1. 三类记录的边界

| 类别 | 用途 | 存储 |
|------|------|------|
| **Run Events** | 业务事件流（SSE + 落库 + 回放） | `run_events` 表（按月分区） |
| **Application Logs** | 运维排障（服务健康、异常、耗时） | stdout JSON |
| **Audit Logs** | 安全审计（管理动作、访问、权限变更） | `audit_logs` 表 |

本方案重点讨论 Application Logs，并要求它与 Run Events 的字段协同。

## 2. 代码归属

| 服务 | 路径 |
|------|------|
| API | `src/services/api/internal/observability/` + `src/services/api/internal/http/` |
| Worker | `src/services/worker/internal/app/`（logger 配置 + trace_id 透传） |

原则：
- 核心逻辑通过 Run Events 表达过程，不直接依赖 logging
- 由 API/Worker 的 composition root 配置日志，业务模块只拿到已配置的 logger / context accessor

## 3. 结构化 JSON（stdout）

所有服务输出单行 JSON 到 stdout：
- 时间戳统一 ISO8601（UTC）
- `exception`/`stack` 序列化为字段
- 兼容 Loki/ELK/Datadog 采集

## 4. trace_id 全链路贯穿

### 4.1 生成与传播

- HTTP 入口（TraceMiddleware）生成 `trace_id`
- 写入：日志字段、API 错误响应、HTTP Header `X-Trace-Id`、Run Events
- 进入 Worker 时通过 `jobs.payload_json` 透传 `trace_id`，Worker 侧恢复上下文

### 4.2 信任策略

- 普通客户端的 `trace_id` 不可信
- 受信任上游（网关）：`ARKLOOP_TRUST_INCOMING_TRACE_ID=1`
- 客户端 IP：`ARKLOOP_TRUST_X_FORWARDED_FOR=1`（仅在反向代理场景启用）

### 4.3 关联 ID 区分

| ID | 说明 |
|----|------|
| `trace_id` | 全链路追踪 |
| `request_id` | 单次 HTTP 请求 |
| `run_id` | Agent Loop 执行实例 |

## 5. 上下文自动注入

日志通过上下文绑定自动补齐字段，不靠逐行手写：

必选上下文字段（优先级从高到低）：
- `trace_id`、`request_id`
- `org_id`、`user_id`
- `project_id`、`thread_id`、`run_id`
- `tool_call_id`、`event_id`
- `component`（`api` / `worker` / `gateway`）

## 6. 脱敏策略

**绝不记录：**
- `Authorization`、`Cookie`、模型厂商 Key、System Prompt 原文

**工具参数/输出：**
- 应用日志只记 `tool_name`、耗时、错误分类
- 明文参数进入 Run Events（按策略脱敏/分级）

**用户输入/模型输出：**
- 应用日志只记长度/摘要
- 回放与审计以 Run Events 为准

## 7. 日志字段 Schema

字段命名：蛇形 + 小写（`trace_id`，不是 `traceId`）。

| 分类 | 字段 |
|------|------|
| 通用 | `ts`、`level`、`logger`、`msg`、`component`、`env`、`version` |
| 关联 | `trace_id`、`request_id`、`org_id`、`user_id`、`project_id`、`thread_id`、`run_id` |
| 执行 | `duration_ms`、`attempt`、`timeout_ms` |
| 工具 | `tool_name`、`tool_call_id`、`risk_level` |
| 费用 | `provider`、`model`、`input_tokens`、`output_tokens`、`cost_usd` |
| 错误 | `error_class`、`error_code`、`exception`、`stack` |

## 8. 错误分类

日志与 API 错误模型对齐：

| 分类 | 说明 |
|------|------|
| `auth.*` | 鉴权/权限 |
| `validation.*` | schema 校验 |
| `policy.*` | 策略拦截 |
| `budget.*` | 预算/配额 |
| `provider.*` | 模型提供商错误 |
| `mcp.*` | MCP 协议错误 |
| `internal.*` | 内部错误 |

额外字段：`retryable`（可重试标记）、`duration_ms`、`cost_usd`。

## 9. Run Events 与应用日志的分工

| 场景 | 写入目标 |
|------|----------|
| 谁调用了什么工具、参数/结果、策略拦截、预算变化 | Run Events |
| 依赖异常、超时、数据库错误、Worker 崩溃、上游不稳定 | 应用日志 |

同一事实两边都需要时：
- Run Events 保留业务语义
- 应用日志只保留关联字段 + 耗时，不重复写敏感明文

## 10. 审计日志

`audit_logs` 表记录所有管理操作：

| 字段 | 说明 |
|------|------|
| `user_id` | 操作人 |
| `action` | 动作类型 |
| `resource_type` | 资源类型 |
| `ip_address` | 来源 IP |
| `user_agent` | 客户端标识 |

任何越权查看/导出/策略变更都必须落审计日志。

## 11. OpenTelemetry 演进路径

当前：以 `trace_id` 贯穿全链路。

后续：OTel 作为增强接入，要求与日志字段对齐（`trace_id` / `span_id`）。引入时不破坏现有日志 schema。
