# API Keys

程序化访问密钥，用于服务端或脚本调用 API（无需用户登录态）。

所有端点需要 Bearer Token 认证。

## 创建 API Key

```http
POST /v1/api-keys
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | 显示名称 |
| `scopes` | `[]string` | 否 | 权限范围（默认继承创建者权限） |

**响应** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "user_id": "...",
  "name": "CI Bot",
  "key_prefix": "al_xxxx",
  "key": "al_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "scopes": [],
  "revoked_at": null,
  "last_used_at": null,
  "created_at": "2024-01-01T00:00:00Z"
}
```

> `key` 字段仅在创建时返回一次，请妥善保存。

---

## 列出 API Keys

```http
GET /v1/api-keys
```

**响应**（不含 `key` 明文）

```json
[
  {
    "id": "...",
    "org_id": "...",
    "user_id": "...",
    "name": "CI Bot",
    "key_prefix": "al_xxxx",
    "scopes": [],
    "revoked_at": null,
    "last_used_at": "2024-01-15T10:00:00Z",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

---

## 撤销 API Key

```http
DELETE /v1/api-keys/{id}
```

响应 `204 No Content`。撤销后该 Key 立即失效。
