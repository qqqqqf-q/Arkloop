# Agent Configs & Prompt 模板

所有端点需要 Bearer Token（或 API Key）认证。

---

## Prompt 模板

### 创建模板

```http
POST /v1/prompt-templates
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | |
| `content` | `string` | 否 | 模板内容（支持 `{{variable}}` 占位符） |
| `variables` | `[]string` | 否 | 声明的变量名列表 |
| `is_default` | `bool` | 否 | 是否为默认模板 |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "name": "法律顾问系统提示",
  "content": "你是一位专业的...",
  "variables": [],
  "is_default": false,
  "version": 1,
  "published_at": null,
  "created_at": "2024-01-01T00:00:00Z"
}
```

### 列出模板

```http
GET /v1/prompt-templates
```

### 获取模板

```http
GET /v1/prompt-templates/{id}
```

### 更新模板

```http
PATCH /v1/prompt-templates/{id}
```

**请求体**（所有字段可选）

| 字段 | 类型 |
|------|------|
| `name` | `string` |
| `content` | `string` |
| `is_default` | `bool` |

### 删除模板

```http
DELETE /v1/prompt-templates/{id}
```

---

## Agent 配置

### 创建配置

```http
POST /v1/agent-configs
```

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `string` | 是 | |
| `scope` | `string` | 否 | `org`（默认）或 `platform`（需 platform_admin） |
| `system_prompt_template_id` | `string` | 否 | 关联 Prompt 模板 |
| `system_prompt_override` | `string` | 否 | 直接覆盖 System Prompt |
| `model` | `string` | 否 | 默认模型 |
| `temperature` | `float64` | 否 | |
| `max_output_tokens` | `int` | 否 | |
| `top_p` | `float64` | 否 | |
| `context_window_limit` | `int` | 否 | |
| `tool_policy` | `string` | 否 | `allowlist`/`denylist`/`none` |
| `tool_allowlist` | `[]string` | 否 | 允许的工具列表 |
| `tool_denylist` | `[]string` | 否 | 禁止的工具列表 |
| `content_filter_level` | `string` | 否 | |
| `project_id` | `string` | 否 | 关联项目 |
| `skill_id` | `string` | 否 | 关联 Skill |
| `is_default` | `bool` | 否 | 是否为默认配置 |
| `prompt_cache_control` | `string` | 否 | `none`（默认）或 `system_prompt` |

**响应**

```json
{
  "id": "...",
  "org_id": "...",
  "scope": "org",
  "name": "标准法律顾问",
  "system_prompt_template_id": "...",
  "system_prompt_override": null,
  "model": "claude-3-5-sonnet-20241022",
  "temperature": null,
  "max_output_tokens": null,
  "top_p": null,
  "context_window_limit": null,
  "tool_policy": "none",
  "tool_allowlist": [],
  "tool_denylist": [],
  "content_filter_level": "",
  "safety_rules_json": null,
  "project_id": null,
  "skill_id": null,
  "is_default": false,
  "prompt_cache_control": "none",
  "created_at": "2024-01-01T00:00:00Z"
}
```

### 列出配置

```http
GET /v1/agent-configs
```

### 获取配置

```http
GET /v1/agent-configs/{id}
```

### 更新配置

```http
PATCH /v1/agent-configs/{id}
```

**请求体** — 所有字段均可选，只更新提供的字段。

### 删除配置

```http
DELETE /v1/agent-configs/{id}
```
