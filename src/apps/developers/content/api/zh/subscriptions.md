---
title: "Subscriptions & Plans"
---
## Plans（套餐）

### 列出套餐

```http
GET /v1/plans
```

**响应**

```json
[
  {
    "id": "...",
    "name": "starter",
    "display_name": "入门版",
    "created_at": "2024-01-01T00:00:00Z",
    "entitlements": [
      {
        "id": "...",
        "key": "credits.monthly_grant",
        "value": "1000",
        "value_type": "integer"
      }
    ]
  }
]
```

### 获取套餐

```http
GET /v1/plans/{id}
```

### 创建套餐

```http
POST /v1/plans
```

需要 `platform_admin` 权限。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | 内部名称（唯一） |
| `display_name` | `string` | 是 | 显示名称 |
| `entitlements` | `array` | 否 | 初始权益列表 |

**权益对象**

| 字段 | 类型 | 说明 |
|------|------|------|
| `key` | `string` | 权益键 |
| `value` | `string` | 权益值 |
| `value_type` | `string` | `integer`/`boolean`/`string` |

---

## Subscriptions（订阅）

### 创建订阅

```http
POST /v1/subscriptions
```

需要 `platform_admin` 权限。

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `org_id` | `string` | 是 |
| `plan_id` | `string` | 是 |
| `current_period_start` | `string` | 是 |
| `current_period_end` | `string` | 是 |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "plan_id": "...",
  "status": "active",
  "current_period_start": "2024-01-01T00:00:00Z",
  "current_period_end": "2024-02-01T00:00:00Z",
  "cancelled_at": null,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### 列出订阅

```http
GET /v1/subscriptions
```

### 获取订阅

```http
GET /v1/subscriptions/{id}
```

### 取消订阅

```http
DELETE /v1/subscriptions/{id}
```
