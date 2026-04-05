---
title: "Webhooks"
---
Webhook 端点用于接收 Arkloop 的事件推送。所有端点需要 Bearer Token 认证。

## 创建 Webhook 端点

```http
POST /v1/webhook-endpoints
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | `string` | 是 | 接收事件的 HTTPS URL |
| `events` | `[]string` | 是 | 订阅的事件类型列表 |

**响应** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "url": "https://example.com/webhooks/arkloop",
  "events": ["run.completed", "run.failed"],
  "enabled": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 列出 Webhook 端点

```http
GET /v1/webhook-endpoints
```

---

## 获取 Webhook 端点

```http
GET /v1/webhook-endpoints/{endpoint_id}
```

---

## 更新 Webhook 端点

```http
PATCH /v1/webhook-endpoints/{endpoint_id}
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `enabled` | `bool` | 启用/禁用 |

---

## 删除 Webhook 端点

```http
DELETE /v1/webhook-endpoints/{endpoint_id}
```

**响应**

```json
{ "ok": true }
```
