---
---

# Projects

All endpoints require Bearer Token (or API Key) authentication.

## Create Project

```http
POST /v1/projects
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | Project name |
| `description` | `string` | No | Description |
| `team_id` | `string` | No | Associated team ID |
| `visibility` | `string` | No | `private` (default) or `shared` |

**Response** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "team_id": null,
  "name": "Legal Counsel Project",
  "description": "...",
  "visibility": "private",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## List Projects

```http
GET /v1/projects
```

Returns a list of projects visible to the current user.

---

## Get Project Details

```http
GET /v1/projects/{project_id}
```

---

## Get Project Default Workspace

```http
GET /v1/projects/{project_id}/workspace
```

Required permission: `data.projects.read`

**Response** `200 OK`

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

`active_session` is returned only when the workspace currently has a live session. `status` is one of `active`, `idle`, or `unavailable`.

---

## List Project Workspace Directory

```http
GET /v1/projects/{project_id}/workspace/files?path=/src
```

Required permissions: `data.projects.read` + `data.runs.read`

`path` is optional and defaults to the root directory `/`.

**Response** `200 OK`

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

Directories are sorted before files. When no manifest has been produced yet, or the manifest object is missing, the endpoint still returns `200` with an empty `items` array.

---

## Read Project Workspace File

```http
GET /v1/projects/{project_id}/workspace/file?path=/src/main.go
```

Required permissions: `data.projects.read` + `data.runs.read`

On success the endpoint returns the raw file contents, and `Content-Type` is inferred from the file extension or content.

**Common Errors**

| HTTP Status | Code | Description |
|------|------|------|
| `400` | `workspace_files.invalid_path` | `path` is invalid or escapes the workspace root |
| `404` | `projects.not_found` | Project does not exist or is outside the current org |
| `404` | `workspace_files.not_found` | File does not exist, target is a directory, or manifest/blob is missing |
