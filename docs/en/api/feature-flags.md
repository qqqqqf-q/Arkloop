# Feature Flags

Feature flag management. Requires `platform_admin` permission.

## Create Feature Flag

```http
POST /v1/feature-flags
```

**Request Body**

| Field | Type | Required |
|------|------|------|
| `key` | `string` | Yes |
| `description` | `string` | No |
| `default_value` | `bool` | Yes |

**Response** `201 Created`

```json
{
  "id": "...",
  "key": "feature.new_dashboard",
  "description": "New version of the dashboard",
  "default_value": false,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## List Feature Flags

```http
GET /v1/feature-flags
```

---

## Get Feature Flag

```http
GET /v1/feature-flags/{key}
```

---

## Update Feature Flag

```http
PATCH /v1/feature-flags/{key}
```

**Request Body**

| Field | Type |
|------|------|
| `default_value` | `bool` |

---

## Delete Feature Flag

```http
DELETE /v1/feature-flags/{key}
```

**Response**

```json
{ "ok": true }
```

---

## Organization-level Override

### Set Override

```http
POST /v1/feature-flags/{key}/org-overrides
```

**Request Body**

| Field | Type | Required | Description |
|------|------|------|------|
| `org_id` | `string` | Yes | Target organization |
| `enabled` | `bool` | Yes | Override value for this organization |

**Response**

```json
{
  "org_id": "...",
  "flag_key": "feature.new_dashboard",
  "enabled": true,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### List Overrides

```http
GET /v1/feature-flags/{key}/org-overrides
```

### Delete Override

```http
DELETE /v1/feature-flags/{key}/org-overrides/{org_id}
```

**Response**

```json
{ "ok": true }
```
