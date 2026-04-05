---
title: "MCP Configs"
---
管理 MCP（Model Context Protocol）服务器配置。所有端点需要 Bearer Token 认证。

## 创建 MCP 配置

```http
POST /v1/mcp-configs
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | 显示名称 |
| `transport` | `string` | 是 | `stdio`/`http_sse`/`streamable_http` |
| `url` | `string` | 条件 | `transport != stdio` 时必填 |
| `bearer_token` | `string` | 否 | HTTP 认证 Token（加密存储） |
| `command` | `string` | 条件 | `transport == stdio` 时必填 |
| `args` | `[]string` | 否 | 命令行参数 |
| `cwd` | `string` | 否 | 工作目录 |
| `env` | `object` | 否 | 额外环境变量 |
| `inherit_parent_env` | `bool` | 否 | 是否继承父进程环境变量 |
| `call_timeout_ms` | `int` | 否 | 调用超时（ms），默认 `10000` |

**响应** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "name": "文件系统工具",
  "transport": "stdio",
  "url": null,
  "has_auth": false,
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
  "cwd": null,
  "inherit_parent_env": false,
  "call_timeout_ms": 10000,
  "is_active": true,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

> `has_auth` 为 `true` 表示已配置 `bearer_token`，但响应不返回 Token 明文。

---

## 列出 MCP 配置

```http
GET /v1/mcp-configs
```

---

## 更新 MCP 配置

```http
PATCH /v1/mcp-configs/{config_id}
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | `string` | |
| `url` | `string \| null` | |
| `bearer_token` | `string \| null` | `null` 清除 Token |
| `call_timeout_ms` | `int` | |
| `is_active` | `bool` | 启用/禁用 |

---

## 删除 MCP 配置

```http
DELETE /v1/mcp-configs/{config_id}
```
