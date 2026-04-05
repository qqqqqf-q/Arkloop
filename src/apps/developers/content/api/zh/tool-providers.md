---
title: "Tool Providers"
---
管理内置 Tool Group（例如 `web_search` / `web_fetch`）的后端 Provider、凭证与 base_url。

Tool Provider 的配置分两层：
- `scope=platform`：平台全局默认（适合自托管/开源落地；新 org 无配置也可直接使用）
- `scope=org`：组织覆盖（租户自定义）

## 列出 Tool Providers

```http
GET /v1/tool-providers?scope=platform
```

`scope`：
- `platform`（默认）：需要 `platform_admin`
- `org`：需要 org 内 `data.secrets` 权限（用于管理敏感凭证）

**响应** `200 OK`

```json
{
  "groups": [
    {
      "group_name": "web_search",
      "providers": [
        {
          "group_name": "web_search",
          "provider_name": "web_search.tavily",
          "is_active": true,
          "key_prefix": "tvly-****1234",
          "requires_api_key": true,
          "requires_base_url": false,
          "configured": true
        },
        {
          "group_name": "web_search",
          "provider_name": "web_search.searxng",
          "is_active": false,
          "base_url": null,
          "requires_api_key": false,
          "requires_base_url": true,
          "configured": false
        }
      ]
    }
  ]
}
```

字段说明：
- `is_active`：当前 scope 内是否激活（同一 scope + group 最多一个 active）
- `configured`：是否已满足该 provider 的必填字段（API Key / Base URL）
- `key_prefix`：仅用于展示，不返回明文 key

## 激活 Provider

```http
PUT /v1/tool-providers/{group}/{provider}/activate?scope=platform
```

行为：
- 在同一 `scope + group` 内原子切换 active provider
- 成功返回 `204 No Content`

## 停用 Provider

```http
PUT /v1/tool-providers/{group}/{provider}/deactivate?scope=platform
```

成功返回 `204 No Content`。

## 写入/更新凭证与 Base URL

```http
PUT /v1/tool-providers/{group}/{provider}/credential?scope=platform
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `api_key` | `string` | 否 | 写入则覆盖现有 key（加密存储） |
| `base_url` | `string` | 否 | 写入则覆盖现有 base_url（自动去掉尾部 `/`） |

说明：
- `web_search.searxng` 必须提供 `base_url`
- `web_search.tavily` / `web_fetch.jina` / `web_fetch.firecrawl` 必须提供 `api_key`
- 同时缺失 `api_key` 与 `base_url` 时，接口返回 `204` 且不做变更

成功返回 `204 No Content`。

## 清空凭证

```http
DELETE /v1/tool-providers/{group}/{provider}/credential?scope=platform
```

清空行为：
- 删除对应 `secret`（同 scope）
- 解除 `tool_provider_configs.secret_id` 与 `key_prefix`

成功返回 `204 No Content`。

## 运行时解析优先级（Worker）

`web_search` / `web_fetch` 在 LLM 侧只暴露 Tool Group 名。Worker 会按优先级解析到最终 Provider：

1) org scope active provider  
2) platform scope active provider  
3) legacy group executor（Config Resolver 的 `web_search.*` / `web_fetch.*`）  
4) 否则返回 `tool.not_configured`

