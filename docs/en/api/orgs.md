# Organizations (Orgs)

All endpoints require Bearer Token authentication.

## Create Organization (Workspace)

```http
POST /v1/orgs
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `slug` | `string` | Yes | Unique identifier (URL friendly) |
| `name` | `string` | Yes | Display name |

**Response**

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

## Get Current User's Organization List

```http
GET /v1/orgs/me
```

---

## Get Organization Details

```http
GET /v1/orgs/{org_id}
```

---

## Get Organization Usage (Monthly)

```http
GET /v1/orgs/{org_id}/usage
```

Query parameters are the same as [`GET /v1/me/usage`](./me#usage-statistics-monthly).

---

## Get Organization Usage (Daily)

```http
GET /v1/orgs/{org_id}/usage/daily
```

---

## Get Organization Usage (by Model)

```http
GET /v1/orgs/{org_id}/usage/by-model
```

---

## Team Management

### Create Team

```http
POST /v1/teams
```

**Request Body**

| Field | Type | Required |
|------|------|------|
| `name` | `string` | Yes |

**Response**

```json
{
  "id": "...",
  "org_id": "...",
  "name": "Legal Team",
  "members_count": 0,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### List Teams

```http
GET /v1/teams
```

### Delete Team

```http
DELETE /v1/teams/{team_id}
```

### List Team Members

```http
GET /v1/teams/{team_id}/members
```

**Response**

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

### Add Team Member

```http
POST /v1/teams/{team_id}/members
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `user_id` | `string` | Yes | |
| `role` | `string` | Yes | `member` or `admin` |

### Remove Team Member

```http
DELETE /v1/teams/{team_id}/members/{user_id}
```

---

## Invitation Management

### Send Invitation

```http
POST /v1/orgs/{org_id}/invitations
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `email` | `string` | Yes | Invited email |
| `role` | `string` | Yes | Assigned role |

**Response**

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

### List Invitations

```http
GET /v1/orgs/{org_id}/invitations
```

### Accept Invitation

```http
POST /v1/org-invitations/{token}/accept
```

**Response**

```json
{ "ok": true }
```

### Revoke Invitation

```http
DELETE /v1/org-invitations/{invitation_id}
```
