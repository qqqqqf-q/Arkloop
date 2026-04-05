---
title: "Messages"
---
All endpoints require Bearer Token (or API Key) authentication.

## Send Message

```http
POST /v1/threads/{thread_id}/messages
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `content` | `string` | Yes | Message content (user input) |

**Response**

```json
{
  "id": "...",
  "org_id": "...",
  "thread_id": "...",
  "created_by_user_id": "...",
  "role": "user",
  "content": "Help me analyze this contract",
  "created_at": "2024-01-01T00:00:00Z"
}
```

> After sending a message, you typically need to create a Run ([`POST /v1/threads/{thread_id}/runs`](./runs)) to trigger an Agent response.

---

## List Messages

```http
GET /v1/threads/{thread_id}/messages
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `limit` | `int` | Number of items per page, defaults to 50 |

**Response**

```json
[
  {
    "id": "...",
    "org_id": "...",
    "thread_id": "...",
    "created_by_user_id": null,
    "role": "assistant",
    "content": "According to Article 3 of the contract...",
    "created_at": "2024-01-01T00:00:01Z"
  }
]
```

| Field | Type | Description |
|------|------|------|
| `role` | `string` | `user` or `assistant` |
| `created_by_user_id` | `string \| null` | Set for user messages, `null` for assistant messages |
