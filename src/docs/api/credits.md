# Credits & Usage

## 获取我的积分

详见 [当前用户 — 获取我的积分](./me#获取我的积分)。

---

## 组织用量统计

详见 [组织 — 用量统计](./orgs#获取组织用量月度)。

---

## 管理员：查询积分

```http
GET /v1/admin/credits?org_id={org_id}
```

需要 `platform_admin` 权限。

**响应**

```json
{
  "org_id": "...",
  "balance": 5000,
  "transactions": [...]
}
```

---

## 管理员：调整积分

```http
POST /v1/admin/credits/adjust
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `org_id` | `string` | 是 | 目标组织 ID |
| `amount` | `int64` | 是 | 调整量（正增负减） |
| `note` | `string` | 否 | 备注 |

**响应**

```json
{
  "org_id": "...",
  "balance": 5100
}
```

---

## 管理员：批量调整积分（所有组织）

```http
POST /v1/admin/credits/bulk-adjust
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `amount` | `int64` | 是 | 调整量 |
| `note` | `string` | 否 | 备注 |

**响应**

```json
{ "affected": 42 }
```

---

## 管理员：重置所有组织积分

```http
POST /v1/admin/credits/reset-all
```

**请求体**

| 字段 | 类型 |
|------|------|
| `note` | `string` |

**响应**

```json
{ "affected": 42 }
```

---

## 管理员：平台用量统计

### 按日

```http
GET /v1/admin/usage/daily?start=YYYY-MM-DD&end=YYYY-MM-DD
```

### 月度摘要

```http
GET /v1/admin/usage/summary?year=2024&month=1
```

### 按模型

```http
GET /v1/admin/usage/by-model?year=2024&month=1
```
