# Runs

A Run is an execution instance of the Agent Loop. All endpoints require Bearer Token (or API Key) authentication.

## Create Run

```http
POST /v1/threads/{thread_id}/runs
```

Starts an Agent execution in the specified thread.

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `route_id` | `string` | No | Provider route ID, specifies model selection if provided |
| `persona_id` | `string` | No | Persona ID, format `persona_key@version`, defaults if empty |

**Response** `201 Created`

```json
{
  "run_id": "...",
  "trace_id": "..."
}
```

---

## List Runs under a Thread

```http
GET /v1/threads/{thread_id}/runs
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `limit` | `int` | Maximum number of items to return |

**Response**

```json
[
  {
    "run_id": "...",
    "status": "completed",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

---

## Global Run List

```http
GET /v1/runs
```

Lists all Runs under the current organization.

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `limit` | `int` | Number of items per page |
| `before` | `string` | cursor |
| `status` | `string` | Status filter: `running`/`completed`/`failed`/`cancelled` |

**Response**

```json
[
  {
    "run_id": "...",
    "org_id": "...",
    "thread_id": "...",
    "status": "completed",
    "model": "claude-3-5-sonnet",
    "persona_id": null,
    "total_input_tokens": 1000,
    "total_output_tokens": 500,
    "total_cost_usd": 0.005,
    "duration_ms": 3200,
    "cache_hit_rate": 0.4,
    "credits_used": 5,
    "created_at": "2024-01-01T00:00:00Z",
    "completed_at": "2024-01-01T00:00:03Z",
    "failed_at": null,
    "created_by_user_id": "...",
    "created_by_user_name": "alice",
    "created_by_email": "alice@example.com"
  }
]
```

---

## Get Run Details

```http
GET /v1/runs/{run_id}
```

**Response**

```json
{
  "run_id": "...",
  "org_id": "...",
  "thread_id": "...",
  "created_by_user_id": "...",
  "status": "completed",
  "created_at": "2024-01-01T00:00:00Z",
  "trace_id": "..."
}
```

---

## Cancel Run

```http
POST /v1/runs/{run_id}:cancel
```

**Response**

```json
{ "ok": true }
```

---

## Submit Interactive Input

```http
POST /v1/runs/{run_id}/input
```

When a Run is in the `waiting_for_input` state, submit user interactive input (such as tool confirmation).

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `content` | `string` | Yes | User input content, maximum 32KB |

**Response**

```json
{ "ok": true }
```

---

## SSE Event Stream

```http
GET /v1/runs/{run_id}/events
```

Receive real-time events during Run execution via Server-Sent Events.

**Request Headers**

```http
Accept: text/event-stream
Authorization: Bearer <token>
```

**Query Parameters**

| Parameter | Type | Description |
|------|------|------|
| `after_seq` | `int` | Start after the specified sequence number (for reconnection) |

**Event Format**

```
data: {"seq":1,"type":"run.started","data":{...},"created_at":"..."}

data: {"seq":2,"type":"message.delta","data":{"content":"..."}}

data: {"seq":99,"type":"run.completed","data":{}}
```

**Primary Event Types**

| Event Type | Description |
|---------|------|
| `run.started` | Run started |
| `message.delta` | Model output delta |
| `tool.call` | Tool call |
| `tool.result` | Tool result |
| `run.waiting_for_input` | Waiting for user input |
| `run.completed` | Run completed successfully |
| `run.failed` | Run failed |
| `run.cancelled` | Run cancelled |

For more details, see [API & SSE Specification](/en/specs/api-and-sse).

---

## Retry (Retry the previous round of conversation)

```http
POST /v1/threads/{thread_id}:retry
```

Deletes the last Assistant message and recreates the Run.

**Response** — Same as Create Run.
