# 组织 (Orgs)

所有端点需要 Bearer Token 认证。

## 创建组织（工作区）

```http
POST /v1/orgs
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `slug` | `string` | 是 | 唯一标识符（URL 友好） |
| `name` | `string` | 是 | 显示名称 |

**响应**

```json
{
  "id": "...",
  "slug": "acme-corp",
  "name": "Acme Corp",
  "type": "workspace",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 获取当前用户的组织列表

```http
GET /v1/orgs/me
```

---

## 获取组织详情

```http
GET /v1/orgs/{org_id}
```

---

## 获取组织用量（月度）

```http
GET /v1/orgs/{org_id}/usage
```

查询参数同 [`GET /v1/me/usage`](./me#用量统计月度)。

---

## 获取组织用量（按日）

```http
GET /v1/orgs/{org_id}/usage/daily
```

---

## 获取组织用量（按模型）

```http
GET /v1/orgs/{org_id}/usage/by-model
```

---

## 团队管理

### 创建团队

```http
POST /v1/teams
```

**请求体**

| 字段 | 类型 | 必填 |
|------|------|------|
| `name` | `string` | 是 |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "name": "法务团队",
  "members_count": 0,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### 列出团队

```http
GET /v1/teams
```

### 删除团队

```http
DELETE /v1/teams/{team_id}
```

### 列出团队成员

```http
GET /v1/teams/{team_id}/members
```

**响应**

```json
[
  {
    "team_id": "...",
    "user_id": "...",
    "role": "member",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

### 添加团队成员

```http
POST /v1/teams/{team_id}/members
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_id` | `string` | 是 | |
| `role` | `string` | 是 | `member` 或 `admin` |

### 移除团队成员

```http
DELETE /v1/teams/{team_id}/members/{user_id}
```

---

## 邀请管理

### 发送邀请

```http
POST /v1/orgs/{org_id}/invitations
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | `string` | 是 | 受邀邮箱 |
| `role` | `string` | 是 | 分配角色 |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "invited_by_user_id": "...",
  "email": "bob@example.com",
  "role": "member",
  "expires_at": "2024-02-01T00:00:00Z",
  "accepted_at": null,
  "created_at": "2024-01-01T00:00:00Z",
  "token": "..."
}
```

### 列出邀请

```http
GET /v1/orgs/{org_id}/invitations
```

### 接受邀请

```http
POST /v1/org-invitations/{token}/accept
```

**响应**

```json
{ "ok": true }
```

### 撤销邀请

```http
DELETE /v1/org-invitations/{invitation_id}
```
