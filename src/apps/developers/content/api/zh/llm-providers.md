---
title: "LLM Providers"
---
管理提供商账号以及该账号下的模型列表。所有端点都需要 Bearer Token 认证。

Provider 是账号级资源，模型挂在 Provider 下面，最终组合成 `provider_name^model_name` 这样的 model selector，供 Persona 直接引用。

## 列出 Providers

```http
GET /v1/llm-providers
```

**响应** `200 OK`

```json
[
  {
    "id": "...",
    "org_id": "...",
    "provider": "anthropic",
    "name": "anthropic-main",
    "key_prefix": "sk-ant-a",
    "base_url": null,
    "openai_api_mode": null,
    "advanced_json": null,
    "created_at": "2024-01-01T00:00:00Z",
    "models": [
      {
        "id": "...",
        "provider_id": "...",
        "model": "claude-sonnet-4-20250514",
        "priority": 0,
        "is_default": true,
        "tags": [],
        "when": {},
        "advanced_json": {},
        "multiplier": 1.0,
        "cost_per_1k_input": 0.003,
        "cost_per_1k_output": 0.015,
        "cost_per_1k_cache_write": null,
        "cost_per_1k_cache_read": null
      }
    ]
  }
]
```

---

## 创建 Provider

```http
POST /v1/llm-providers
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | Provider 显示名，也是 selector 前缀；不能包含 `^` |
| `provider` | `string` | 是 | `openai` 或 `anthropic` |
| `api_key` | `string` | 是 | API Key（加密存储） |
| `base_url` | `string` | 否 | 自定义端点 |
| `openai_api_mode` | `string` | 否 | `auto` / `responses` / `chat_completions` |
| `advanced_json` | `object` | 否 | Provider 额外配置 |

---

## 更新 Provider

```http
PATCH /v1/llm-providers/{provider_id}
```

**请求体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | `string` | 更新 Provider 名称 |
| `provider` | `string` | 更新 Provider 类型 |
| `api_key` | `string` | 覆盖已存储的密钥 |
| `base_url` | `string \| null` | 更新自定义端点 |
| `openai_api_mode` | `string \| null` | 更新 OpenAI 兼容模式 |
| `advanced_json` | `object \| null` | 更新高级配置 |

---

## 删除 Provider

```http
DELETE /v1/llm-providers/{provider_id}
```

删除 Provider 时会一并移除其下的模型列表。

---

## 创建 Provider Model

```http
POST /v1/llm-providers/{provider_id}/models
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | `string` | 是 | 上游模型名 |
| `priority` | `int` | 否 | 越大越优先 |
| `is_default` | `bool` | 否 | 是否为该 Provider 下的默认模型 |
| `tags` | `string[]` | 否 | 扩展标签 |
| `when` | `object` | 否 | 条件路由规则 |
| `advanced_json` | `object` | 否 | 模型级高级配置；与 provider 级按键合并，模型级优先 |
| `multiplier` | `float64` | 否 | 成本倍率 |
| `cost_per_1k_input` | `float64` | 否 | 每 1k input token 成本 |
| `cost_per_1k_output` | `float64` | 否 | 每 1k output token 成本 |
| `cost_per_1k_cache_write` | `float64` | 否 | 缓存写成本 |
| `cost_per_1k_cache_read` | `float64` | 否 | 缓存读成本 |

Persona 后续可直接引用 `anthropic-main^claude-sonnet-4-20250514` 这样的 selector。

---

## 更新 Provider Model

```http
PATCH /v1/llm-providers/{provider_id}/models/{model_id}
```

可更新的字段与“创建 Provider Model”一致。

当 provider 级和模型级同时设置 `advanced_json` 时，Arkloop 会按键合并，冲突时以模型级为准。

---

## 删除 Provider Model

```http
DELETE /v1/llm-providers/{provider_id}/models/{model_id}
```

---

## 列出可用模型

```http
GET /v1/llm-providers/{provider_id}/available-models
```

返回该 Provider 对应上游探测到的模型列表：

```json
{
  "models": [
    {
      "id": "claude-sonnet-4-20250514",
      "name": "claude-sonnet-4-20250514",
      "configured": true
    }
  ]
}
```
