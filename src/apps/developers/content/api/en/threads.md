---
title: "Threads"
---
All endpoints require Bearer Token (or API Key) authentication.

## Create Thread

```http
POST /v1/threads
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `title` | `string` | No | Thread title |
| `is_private` | `bool` | No | Whether it's private, defaults to `false` |

**Response**

```json
{
  "id": "...",
  "org_id": "...",
  "created_by_user_id": "...",
  "title": "My first conversation",
  "project_id": null,
  "created_at": "2024-01-01T00:00:00Z",
  "active_run_id": null,
  "is_private": false
}
```

---

## List Threads

```http
GET /v1/threads
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `limit` | `int` | Number of items per page |
| `before` | `string` | Cursor for pagination |
| `project_id` | `string` | Filter by project |

---

## Search Threads

```http
GET /v1/threads/search
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `q` | `string` | Search keywords |
| `limit` | `int` | Number of items per page |

---

## Starred Threads

```http
GET /v1/threads/starred
```

---

## Get Thread Details

```http
GET /v1/threads/{thread_id}
```

---

## Update Thread

```http
PATCH /v1/threads/{thread_id}
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `title` | `string \| null` | Title (`null` to clear) |
| `project_id` | `string \| null` | Associated project |

---

## Delete Thread

```http
DELETE /v1/threads/{thread_id}
```

---

## Star / Unstar Thread

```http
POST   /v1/threads/{thread_id}/star
DELETE /v1/threads/{thread_id}/star
```
