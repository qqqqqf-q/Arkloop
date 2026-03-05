# Notifications

In-app notifications. All endpoints require Bearer Token authentication.

## List Notifications

```http
GET /v1/notifications
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `unread_only` | `bool` | Return only unread notifications, default `false` |
| `type` | `string` | Filter by notification type |

**Response**

```json
{
  "data": [
    {
      "id": "...",
      "user_id": "...",
      "org_id": "...",
      "type": "broadcast",
      "title": "System Maintenance Notification",
      "body": "Routine maintenance will be performed tonight at 22:00...",
      "payload": {},
      "read_at": null,
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

---

## Mark All Notifications as Read

```http
PATCH /v1/notifications
```

**Response**

```json
{ "ok": true, "count": 5 }
```

---

## Mark Single Notification as Read

```http
PATCH /v1/notifications/{notification_id}
```

**Response**

```json
{ "ok": true }
```

---

## Admin Broadcast

Admins can send broadcast notifications to all users or specific organizations.

### Create Broadcast

```http
POST /v1/admin/notifications/broadcasts
```

Requires `platform_admin` permission.

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `type` | `string` | Yes | Notification type |
| `title` | `string` | Yes | Title |
| `body` | `string` | Yes | Body content |
| `target` | `string` | Yes | `all` or specific `org_id` |
| `payload` | `object` | No | Additional data |

**Response** `202 Accepted`

```json
{
  "id": "...",
  "type": "announcement",
  "title": "New Feature Launched",
  "body": "...",
  "target_type": "all",
  "target_id": null,
  "payload": {},
  "status": "pending",
  "sent_count": 0,
  "created_by": "...",
  "created_at": "2024-01-01T00:00:00Z"
}
```

### List Broadcasts

```http
GET /v1/admin/notifications/broadcasts
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `limit` | `int` | |
| `cursor` | `string` | |

### Get Broadcast Details

```http
GET /v1/admin/notifications/broadcasts/{broadcast_id}
```

### Delete Broadcast

```http
DELETE /v1/admin/notifications/broadcasts/{broadcast_id}
```
