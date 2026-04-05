---
title: "Health Checks"
---
## Liveness Probe

```http
GET /healthz
```

Service liveness probe, no authentication required.

**Response** `200 OK`

```json
{ "status": "ok" }
```

---

## Readiness Probe

```http
GET /readyz
```

Service readiness probe, verifies database connection and schema version. No authentication required.

**Response**

`200 OK` — Service ready:

```json
{ "status": "ok", "schema_version": "00023" }
```

`503 Service Unavailable` — Service not ready (database not connected or schema version mismatch):

```json
{
  "status": "unavailable",
  "reason": "schema version mismatch: expected 00023, got 00020"
}
```
