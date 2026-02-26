# 当前用户 (Me)

所有端点需要 Bearer Token 认证。

## 获取当前用户信息

```http
GET /v1/me
```

**响应**

```json
{
  "id": "...",
  "username": "alice",
  "email": "alice@example.com",
  "email_verified": true,
  "email_verification_required": false,
  "created_at": "2024-01-01T00:00:00Z",
  "org_id": "...",
  "org_name": "Acme Corp",
  "role": "admin",
  "permissions": ["data.threads.read", "data.threads.manage"]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `permissions` | `[]string` | 当前用户在所属组织中的权限点列表 |

---

## 更新当前用户

```http
PATCH /v1/me
```

**请求体**

| 字段 | 类型 |
|------|------|
| `username` | `string` |

**响应**

```json
{ "username": "new_name" }
```

---

## 用量统计（月度）

```http
GET /v1/me/usage
```

**查询参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `year` | `int` | 可选，默认当月 |
| `month` | `int` | 可选，默认当月 |

**响应**

```json
{
  "org_id": "...",
  "year": 2024,
  "month": 1,
  "total_input_tokens": 100000,
  "total_output_tokens": 50000,
  "total_cost_usd": 0.15,
  "record_count": 42
}
```

---

## 用量统计（按日）

```http
GET /v1/me/usage/daily
```

**查询参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `start` | `string` | 是 | 开始日期，格式 `YYYY-MM-DD` |
| `end` | `string` | 是 | 结束日期，格式 `YYYY-MM-DD` |

**响应**

```json
[
  {
    "date": "2024-01-01",
    "input_tokens": 1000,
    "output_tokens": 500,
    "cost_usd": 0.01,
    "record_count": 3
  }
]
```

---

## 用量统计（按模型）

```http
GET /v1/me/usage/by-model
```

**查询参数** — 同月度统计。

**响应**

```json
[
  {
    "model": "claude-3-5-sonnet",
    "input_tokens": 50000,
    "output_tokens": 25000,
    "cost_usd": 0.08,
    "record_count": 20
  }
]
```

---

## 获取我的积分

```http
GET /v1/me/credits
```

**响应**

```json
{
  "balance": 1000,
  "transactions": [
    {
      "id": "...",
      "org_id": "...",
      "amount": 100,
      "type": "credit",
      "reference_type": "admin_adjust",
      "reference_id": "...",
      "note": "初始充值",
      "thread_title": null,
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

---

## 获取我的邀请码

```http
GET /v1/me/invite-code
```

**响应**

```json
{
  "id": "...",
  "user_id": "...",
  "code": "ABC123",
  "max_uses": 10,
  "use_count": 2,
  "is_active": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 重置邀请码

```http
POST /v1/me/invite-code/reset
```

**响应** — 同获取邀请码。

---

## 兑换码

```http
POST /v1/me/redeem
```

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `code` | `string` | 是 |

**响应**

```json
{
  "code": "PROMO2024",
  "type": "credits",
  "value": "100"
}
```
