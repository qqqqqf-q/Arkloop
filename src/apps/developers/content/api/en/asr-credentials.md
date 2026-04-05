---
title: "ASR Credentials"
---
Manage Automatic Speech Recognition (ASR) service credentials. All endpoints require Bearer Token authentication.

## Create Credential

```http
POST /v1/asr-credentials
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `name` | `string` | Yes | Display name |
| `provider` | `string` | Yes | `groq` or `openai` |
| `api_key` | `string` | Yes | API key (stored encrypted) |
| `base_url` | `string` | No | Custom endpoint |
| `model` | `string` | Yes | Model name (e.g., `whisper-large-v3`) |
| `is_default` | `bool` | No | Whether this is the default credential |
| `scope` | `string` | No | `org` (default) or `platform` (requires platform_admin) |

**Response**

```json
{
  "id": "...",
  "org_id": "...",
  "scope": "org",
  "provider": "groq",
  "name": "Groq ASR",
  "key_prefix": "gsk_xxxx",
  "base_url": null,
  "model": "whisper-large-v3",
  "is_default": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## List Credentials

```http
GET /v1/asr-credentials
```

---

## Delete Credential

```http
DELETE /v1/asr-credentials/{id}
```

---

## Set as Default

```http
POST /v1/asr-credentials/{id}/set-default
```

---

## Speech to Text

```http
POST /v1/asr/transcribe
```

Upload audio file via `multipart/form-data`.

| Field | Type | Required | Description |
|------|------|------|------|
| `file` | `file` | Yes | Audio file |
| `language` | `string` | No | Language code (e.g., `zh`) |

**Response**

```json
{
  "text": "Transcribed text content..."
}
```
