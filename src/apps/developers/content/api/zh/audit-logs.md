---
title: "审计日志 (Audit Logs)"
---
所有端点需要 Bearer Token 认证。

## 查询审计日志

```http
GET /v1/audit-logs
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `org_id` | `string` | 按组织过滤（platform_admin 可跨组织查询） |
| `action` | `string` | 操作类型，如 `user.login`、`thread.create` |
| `actor_user_id` | `string` | 操作者 ID |
| `target_type` | `string` | 目标资源类型 |
| `since` | `string` | 起始时间（RFC3339） |
| `until` | `string` | 结束时间（RFC3339） |
| `limit` | `int` | 每页数量 |
| `offset` | `int` | 偏移量 |
| `include` | `string` | 额外字段，`include=state` 返回 before/after 状态 |

**响应**

```json
{
  "data": [
    {
      "id": "...",
      "org_id": "...",
      "actor_user_id": "...",
      "action": "user.login",
      "target_type": "user",
      "target_id": "...",
      "trace_id": "...",
      "metadata": {},
      "ip_address": "1.2.3.4",
      "user_agent": "Mozilla/5.0...",
      "created_at": "2024-01-01T00:00:00Z",
      "before_state": null,
      "after_state": null
    }
  ],
  "total": 1250
}
```

> 仅在 `include=state` 时返回 `before_state` / `after_state`。
