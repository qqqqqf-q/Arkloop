# 项目 (Projects)

所有端点需要 Bearer Token（或 API Key）认证。

## 创建项目

```http
POST /v1/projects
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | 项目名称 |
| `description` | `string` | 否 | 描述 |
| `team_id` | `string` | 否 | 关联团队 ID |
| `visibility` | `string` | 否 | `private`（默认）或 `shared` |

**响应** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "team_id": null,
  "name": "法律顾问项目",
  "description": "...",
  "visibility": "private",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 列出项目

```http
GET /v1/projects
```

返回当前用户可见的项目列表。

---

## 获取项目详情

```http
GET /v1/projects/{project_id}
```
