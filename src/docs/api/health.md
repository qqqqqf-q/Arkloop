# 健康检查

## Liveness Probe

```http
GET /healthz
```

服务存活探针，无需认证。

**响应** `200 OK`

```json
{ "status": "ok" }
```

---

## Readiness Probe

```http
GET /readyz
```

服务就绪探针，验证数据库连接和 Schema 版本。无需认证。

**响应**

`200 OK` — 服务就绪：

```json
{ "status": "ok", "schema_version": "00023" }
```

`503 Service Unavailable` — 服务未就绪（数据库未连接或 Schema 版本不匹配）：

```json
{
  "status": "unavailable",
  "reason": "schema version mismatch: expected 00023, got 00020"
}
```
