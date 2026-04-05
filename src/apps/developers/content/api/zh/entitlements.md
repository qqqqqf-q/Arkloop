---
title: "Entitlements"
---
权益系统用于控制组织的功能访问与资源配额。需要 `platform_admin` 权限。

## 列出权益覆盖

```http
GET /v1/entitlement-overrides?org_id={org_id}
```

**响应**

```json
[
  {
    "id": "...",
    "org_id": "...",
    "key": "credits.monthly_grant",
    "value": "5000",
    "value_type": "integer",
    "reason": "企业客户特批",
    "expires_at": null,
    "created_by_user_id": "...",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

---

## 创建权益覆盖

```http
POST /v1/entitlement-overrides
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `org_id` | `string` | 是 | 目标组织 |
| `key` | `string` | 是 | 权益键 |
| `value` | `string` | 是 | 权益值 |
| `value_type` | `string` | 是 | `integer`/`boolean`/`string` |
| `reason` | `string` | 否 | 操作原因 |
| `expires_at` | `string` | 否 | 过期时间（RFC3339） |

---

## 删除权益覆盖

```http
DELETE /v1/entitlement-overrides/{id}?org_id={org_id}
```

**响应**

```json
{ "ok": true }
```
