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
