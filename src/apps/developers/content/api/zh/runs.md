---
title: "运行 (Runs)"
---
Run 是一次 Agent Loop 执行实例。所有端点需要 Bearer Token（或 API Key）认证。

## 创建 Run

```http
POST /v1/threads/{thread_id}/runs
```

在指定线程中启动一次 Agent 执行。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `route_id` | `string` | 否 | Provider 路由 ID，指定后按此路由选择模型 |
| `persona_id` | `string` | 否 | Persona ID，格式 `persona_key@version`，不填则使用默认 |

**响应** `201 Created`

```json
{
  "run_id": "...",
  "trace_id": "..."
}
```

---

## 列出线程下的 Run

```http
GET /v1/threads/{thread_id}/runs
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `limit` | `int` | 最多返回条数 |

**响应**

```json
[
  {
    "run_id": "...",
    "status": "completed",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

---

## 全局 Run 列表

```http
GET /v1/runs
```

列出当前组织下的所有 Run。

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `limit` | `int` | 每页数量 |
| `before` | `string` | cursor |
| `status` | `string` | 过滤状态：`running`/`completed`/`failed`/`cancelled` |

**响应**

```json
[
  {
    "run_id": "...",
    "org_id": "...",
    "thread_id": "...",
    "status": "completed",
    "model": "claude-3-5-sonnet",
    "persona_id": null,
    "total_input_tokens": 1000,
    "total_output_tokens": 500,
    "total_cost_usd": 0.005,
    "duration_ms": 3200,
    "cache_hit_rate": 0.4,
    "credits_used": 5,
    "created_at": "2024-01-01T00:00:00Z",
    "completed_at": "2024-01-01T00:00:03Z",
    "failed_at": null,
    "created_by_user_id": "...",
    "created_by_user_name": "alice",
    "created_by_email": "alice@example.com"
  }
]
```

---

## 获取 Run 详情

```http
GET /v1/runs/{run_id}
```

**响应**

```json
{
  "run_id": "...",
  "org_id": "...",
  "thread_id": "...",
  "created_by_user_id": "...",
  "status": "completed",
  "created_at": "2024-01-01T00:00:00Z",
  "trace_id": "..."
}
```

---

## 取消 Run

```http
POST /v1/runs/{run_id}:cancel
```

**响应**

```json
{ "ok": true }
```

---

## 提交交互输入

```http
POST /v1/runs/{run_id}/input
```

当 Run 处于 `waiting_for_input` 状态时，提交用户交互输入（如工具确认）。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | `string` | 是 | 用户输入内容，最大 32KB |

**响应**

```json
{ "ok": true }
```

---

## SSE 事件流

```http
GET /v1/runs/{run_id}/events
```

通过 Server-Sent Events 实时接收 Run 执行过程中的事件。

**请求头**

```http
Accept: text/event-stream
Authorization: Bearer <token>
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `after_seq` | `int` | 从指定序号之后开始（用于断线重连） |

**事件格式**

```
data: {"seq":1,"type":"run.started","data":{...},"created_at":"..."}

data: {"seq":2,"type":"message.delta","data":{"content":"..."}}

data: {"seq":99,"type":"run.completed","data":{}}
```

**主要事件类型**

| 事件类型 | 说明 |
|---------|------|
| `run.started` | Run 启动 |
| `message.delta` | 模型输出增量 |
| `tool.call` | 工具调用 |
| `tool.result` | 工具返回 |
| `run.waiting_for_input` | 等待用户输入 |
| `run.completed` | Run 成功完成 |
| `run.failed` | Run 失败 |
| `run.cancelled` | Run 已取消 |

详见 [API & SSE 规范](/docs/specs/api-and-sse)。

---

## Retry（重试上一轮对话）

```http
POST /v1/threads/{thread_id}:retry
```

删除最后一条 Assistant 消息并重新创建 Run。

**响应** — 同创建 Run。
