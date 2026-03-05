# Webhooks

Webhook endpoints for receiving event pushes from Arkloop. All endpoints require Bearer Token authentication.

## Create Webhook Endpoint

```http
POST /v1/webhook-endpoints
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `url` | `string` | Yes | HTTPS URL to receive events |
| `events` | `[]string` | Yes | List of subscribed event types |

**Response** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "url": "https://example.com/webhooks/arkloop",
  "events": ["run.completed", "run.failed"],
  "enabled": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## List Webhook Endpoints

```http
GET /v1/webhook-endpoints
```

---

## Get Webhook Endpoint

```http
GET /v1/webhook-endpoints/{endpoint_id}
```

---

## Update Webhook Endpoint

```http
PATCH /v1/webhook-endpoints/{endpoint_id}
```

**Request Body**

| Field | Type | Description |
|------|------|------|
| `enabled` | `bool` | Enable/Disable |

---

## Delete Webhook Endpoint

```http
DELETE /v1/webhook-endpoints/{endpoint_id}
```

**Response**

```json
{ "ok": true }
```
