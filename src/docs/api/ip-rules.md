# IP 规则

管理组织级别的 IP 访问控制。需要相应权限。

## 创建 IP 规则

```http
POST /v1/ip-rules
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `type` | `string` | 是 | `allowlist` 或 `blocklist` |
| `cidr` | `string` | 是 | CIDR 格式，如 `192.168.1.0/24` |
| `note` | `string` | 否 | 备注 |

**响应** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "type": "allowlist",
  "cidr": "10.0.0.0/8",
  "note": "内网访问",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 列出 IP 规则

```http
GET /v1/ip-rules
```

---

## 删除 IP 规则

```http
DELETE /v1/ip-rules/{rule_id}
```

响应 `204 No Content`。
