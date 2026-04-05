---
title: "Notifications"
---
站内通知。所有端点需要 Bearer Token 认证。

## 列出通知

```http
GET /v1/notifications
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `unread_only` | `bool` | 只返回未读，默认 `false` |
| `type` | `string` | 按通知类型过滤 |

**响应**

```json
{
  "data": [
    {
      "id": "...",
      "user_id": "...",
      "org_id": "...",
      "type": "broadcast",
      "title": "系统维护通知",
      "body": "将于今晚 22:00 进行例行维护...",
      "payload": {},
      "read_at": null,
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

---

## 标记所有通知为已读

```http
PATCH /v1/notifications
```

**响应**

```json
{ "ok": true, "count": 5 }
```

---

## 标记单条通知为已读

```http
PATCH /v1/notifications/{notification_id}
```

**响应**

```json
{ "ok": true }
```

---

## 管理员广播

管理员可向所有用户或特定组织发送广播通知。

### 创建广播

```http
POST /v1/admin/notifications/broadcasts
```

需要 `platform_admin` 权限。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `type` | `string` | 是 | 通知类型 |
| `title` | `string` | 是 | 标题 |
| `body` | `string` | 是 | 正文 |
| `target` | `string` | 是 | `all` 或具体 `org_id` |
| `payload` | `object` | 否 | 附加数据 |

**响应** `202 Accepted`

```json
{
  "id": "...",
  "type": "announcement",
  "title": "新功能上线",
  "body": "...",
  "target_type": "all",
  "target_id": null,
  "payload": {},
  "status": "pending",
  "sent_count": 0,
  "created_by": "...",
  "created_at": "2024-01-01T00:00:00Z"
}
```

### 列出广播

```http
GET /v1/admin/notifications/broadcasts
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `limit` | `int` | |
| `cursor` | `string` | |

### 获取广播详情

```http
GET /v1/admin/notifications/broadcasts/{broadcast_id}
```

### 删除广播

```http
DELETE /v1/admin/notifications/broadcasts/{broadcast_id}
```
