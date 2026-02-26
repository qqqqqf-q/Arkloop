# Feature Flags

功能开关管理。需要 `platform_admin` 权限。

## 创建 Feature Flag

```http
POST /v1/feature-flags
```

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `key` | `string` | 是 |
| `description` | `string` | 否 |
| `default_value` | `bool` | 是 |

**响应** `201 Created`

```json
{
  "id": "...",
  "key": "feature.new_dashboard",
  "description": "新版仪表盘",
  "default_value": false,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 列出 Feature Flags

```http
GET /v1/feature-flags
```

---

## 获取 Feature Flag

```http
GET /v1/feature-flags/{key}
```

---

## 更新 Feature Flag

```http
PATCH /v1/feature-flags/{key}
```

**请求体**

| 字段 | 类型 |
|------|------|
| `default_value` | `bool` |

---

## 删除 Feature Flag

```http
DELETE /v1/feature-flags/{key}
```

**响应**

```json
{ "ok": true }
```

---

## 组织级覆盖

### 设置覆盖

```http
POST /v1/feature-flags/{key}/org-overrides
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `org_id` | `string` | 是 | 目标组织 |
| `enabled` | `bool` | 是 | 对该组织的覆盖值 |

**响应**

```json
{
  "org_id": "...",
  "flag_key": "feature.new_dashboard",
  "enabled": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### 列出覆盖

```http
GET /v1/feature-flags/{key}/org-overrides
```

### 删除覆盖

```http
DELETE /v1/feature-flags/{key}/org-overrides/{org_id}
```

**响应**

```json
{ "ok": true }
```
