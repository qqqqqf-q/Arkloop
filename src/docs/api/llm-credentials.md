# LLM Credentials

管理 LLM 提供商凭证与路由规则。所有端点需要 Bearer Token 认证。

## 创建凭证

```http
POST /v1/llm-credentials
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | 显示名称 |
| `provider` | `string` | 是 | `openai` 或 `anthropic` |
| `api_key` | `string` | 是 | API 密钥（加密存储） |
| `base_url` | `string` | 否 | 自定义端点（兼容第三方） |
| `openai_api_mode` | `string` | 否 | `auto`/`responses`/`chat_completions` |
| `advanced_json` | `object` | 否 | 额外配置 |
| `routes` | `array` | 否 | 初始路由规则（见下方） |

**路由对象 (`routes[*]`)**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | `string` | 是 | 模型名称 |
| `is_default` | `bool` | 否 | 是否为默认路由 |
| `priority` | `int` | 否 | 优先级，数值越大越优先 |
| `when` | `object` | 否 | 条件匹配规则（JSON） |
| `multiplier` | `float64` | 否 | 成本倍率 |
| `cost_per_1k_input` | `float64` | 否 | 每千 input token 成本（USD） |
| `cost_per_1k_output` | `float64` | 否 | 每千 output token 成本（USD） |
| `cost_per_1k_cache_write` | `float64` | 否 | 缓存写入成本 |
| `cost_per_1k_cache_read` | `float64` | 否 | 缓存读取成本 |

**响应** `201 Created`

```json
{
  "id": "...",
  "org_id": "...",
  "provider": "anthropic",
  "name": "主 Anthropic 账号",
  "key_prefix": "sk-ant-a",
  "base_url": null,
  "openai_api_mode": null,
  "advanced_json": null,
  "created_at": "2024-01-01T00:00:00Z",
  "routes": [
    {
      "id": "...",
      "credential_id": "...",
      "model": "claude-3-5-sonnet-20241022",
      "priority": 0,
      "is_default": true,
      "when": null,
      "multiplier": 1.0,
      "cost_per_1k_input": 0.003,
      "cost_per_1k_output": 0.015,
      "cost_per_1k_cache_write": null,
      "cost_per_1k_cache_read": null
    }
  ]
}
```

---

## 列出凭证

```http
GET /v1/llm-credentials
```

---

## 更新凭证

```http
PATCH /v1/llm-credentials/{cred_id}
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | `string` | |
| `provider` | `string` | |
| `base_url` | `string \| null` | |
| `openai_api_mode` | `string \| null` | |
| `api_key` | `string` | 提供则覆盖现有密钥 |
| `advanced_json` | `object` | |

---

## 删除凭证

```http
DELETE /v1/llm-credentials/{cred_id}
```

---

## 更新路由

```http
PATCH /v1/llm-credentials/{cred_id}/routes/{route_id}
```

**请求体** — 同创建凭证中的路由对象格式。
