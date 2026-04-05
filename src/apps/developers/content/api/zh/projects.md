---
title: "项目 (Projects)"
---
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

---

## 获取项目默认工作区

```http
GET /v1/projects/{project_id}/workspace
```

权限要求：`data.projects.read`

**响应** `200 OK`

```json
{
  "project_id": "...",
  "workspace_ref": "ws_org_profile_project_xxx",
  "owner_user_id": "...",
  "status": "idle",
  "last_used_at": "2024-01-01T00:00:00Z",
  "active_session": {
    "session_ref": "shref_xxx",
    "session_type": "shell",
    "state": "ready",
    "last_used_at": "2024-01-01T00:00:00Z"
  }
}
```

`active_session` 仅在工作区存在活动会话时返回；`status` 取值为 `active`、`idle`、`unavailable`。

---

## 列出项目工作区目录

```http
GET /v1/projects/{project_id}/workspace/files?path=/src
```

权限要求：`data.projects.read` + `data.runs.read`

`path` 可省略，默认返回根目录 `/`。

**响应** `200 OK`

```json
{
  "workspace_ref": "ws_org_profile_project_xxx",
  "path": "/src",
  "items": [
    {
      "name": "main.go",
      "path": "/src/main.go",
      "type": "file",
      "size": 14,
      "mtime_unix_ms": 1710000000000,
      "mime_type": "text/plain; charset=utf-8"
    }
  ]
}
```

目录项优先排序；当 manifest 尚未生成或对象缺失时，接口仍返回 `200`，`items` 为空数组。

---

## 读取项目工作区文件

```http
GET /v1/projects/{project_id}/workspace/file?path=/src/main.go
```

权限要求：`data.projects.read` + `data.runs.read`

成功时直接返回文件内容，`Content-Type` 按文件扩展名或内容推断。

**常见错误**

| HTTP 状态 | 错误码 | 说明 |
|------|------|------|
| `400` | `workspace_files.invalid_path` | `path` 非法或越过工作区根目录 |
| `404` | `projects.not_found` | 项目不存在或不属于当前组织 |
| `404` | `workspace_files.not_found` | 文件不存在、目标是目录，或 manifest/blob 缺失 |
