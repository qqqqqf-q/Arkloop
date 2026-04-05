---
title: "消息 (Messages)"
---
所有端点需要 Bearer Token（或 API Key）认证。

## 发送消息

```http
POST /v1/threads/{thread_id}/messages
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | `string` | 是 | 消息内容（用户输入） |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "thread_id": "...",
  "created_by_user_id": "...",
  "role": "user",
  "content": "帮我分析这份合同",
  "created_at": "2024-01-01T00:00:00Z"
}
```

> 发送消息后通常需要创建一个 Run（[`POST /v1/threads/{thread_id}/runs`](./runs)）来触发 Agent 响应。

---

## 列出消息

```http
GET /v1/threads/{thread_id}/messages
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `limit` | `int` | 每页数量，默认 50 |

**响应**

```json
[
  {
    "id": "...",
    "org_id": "...",
    "thread_id": "...",
    "created_by_user_id": null,
    "role": "assistant",
    "content": "根据合同第3条...",
    "created_at": "2024-01-01T00:00:01Z"
  }
]
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `role` | `string` | `user` 或 `assistant` |
| `created_by_user_id` | `string \| null` | 用户消息有值，Assistant 消息为 `null` |
