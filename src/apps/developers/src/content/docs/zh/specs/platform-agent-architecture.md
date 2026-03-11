---
---

# Platform Agent 架构设计

本文定义 Arkloop 内置 Platform Agent 的设计目标、能力边界、工具集与身份模型。

Platform Agent 的核心目标是：自部署用户完成首次 LLM Provider 配置之后，平台的一切后续配置与管理操作，均可通过自然语言完成，无需再打开 Console。

---

## 1. 设计目标

用户在完成以下两步初始化之后：

1. 配置一个可用的 LLM Provider（API Key 或自托管 URL）
2. 获取 Arkloop 的访问 API Key

平台的全部后续操作均可通过 Platform Agent 处理，包括但不限于：

- 配置邮件 SMTP
- 配置 Cloudflare Turnstile 验证码
- 创建和管理 Agent（Persona）
- 安装 Skill
- 接入社交平台 Channel（Telegram / Discord / 飞书）
- 管理 MCP 工具配置
- 更新平台版本
- 管理用户与访问控制

---

## 2. 非目标

- 不替代 Console 的数据查看与审计功能
- 不处理账单与信用额度管理（SaaS 场景）
- 不直接执行宿主机 shell 命令（通过 Installer Bridge 代理）
- 不提供前端视觉编辑能力（如拖拽布局）
- 不自行修改 Platform Agent 自身的 Persona 配置

---

## 3. 在 Sub-agent 架构中的定位

Platform Agent 是 Arkloop Sub-agent 架构中的一个特殊实体，有以下特征：

- `source_type`: `platform_agent`，独立于普通 `thread_spawn`
- `context_mode`: 固定为 `isolated`，不继承用户对话历史
- 身份: 使用系统内置 `system_agent` service account，不继承发起者身份
- 权限: 持有 `RoleSystemAgent` 角色，权限集固定且不可外部修改
- 生命周期: 每次调用为独立 run，任务完成即关闭

顶层 Agent 只暴露一个工具 `call_platform(input: string)`，内部走 `Spawn(platform, isolated) + Wait + Collect`。顶层 Agent 无法直接操作 Platform Agent 的工具，只能通过自然语言输入委托任务。

### 3.1 权限继承例外

Sub-agent 架构的通用规则是"子 Agent 权限不能超过父 Agent"。Platform Agent 是唯一的例外：它的权限由系统在 spawn 时注入，与调用者权限无关。Control Plane 在检测到 `source_type == platform_agent` 时，强制替换为 `system_agent` identity，不继承 `parentRun.CreatedByUserID`。

---

## 4. 身份模型

### 4.1 RoleSystemAgent

```go
const RoleSystemAgent = "system_agent"

var systemAgentPerms = []string{
    PermDataPersonasRead, PermDataPersonasManage,
    PermDataSkillsRead,   PermDataSkillsManage,
    PermDataLLMCreds,
    PermDataMCPConfigs,
    PermDataProjectsRead, PermDataProjectsManage,
    PermDataWebhooksManage,
    PermPlatformFeatureFlagsManage,
    // 通过 platform_settings 覆盖：email、captcha、gateway、styles
}
```

明确排除：

- `PermPlatformAdmin`（平台超级管理员）
- `PermOrgMembersInvite` / `PermOrgMembersRevoke`（用户管理，v1 排除）
- `PermPlatformPlansManage` / `PermPlatformSubscriptionsManage`（计费）
- `PermDataSecrets`（直接读写密钥存储）

### 4.2 system_agent service account

DB 启动时 seed 一个 `system_agent` user，`org_role` 为 `system_agent`，不可登录，不可被删除。`child_run.go`（或新 Control Plane）在 Platform Agent path 下替换 `CreatedByUserID` 为此账号的 UUID。

---

## 5. 工具集

Platform Agent 的工具分为以下八个域，每个工具对应一个或多个现有 API 端点。

### 5.1 基础设施（via Installer Bridge）

| 工具 | 对应操作 |
|------|---------|
| `get_platform_status` | 检查各服务健康状态 |
| `list_installed_modules` | 列出已安装的可选模块（Sandbox、OpenViking、Browser） |
| `install_module(name)` | 安装可选模块 |
| `trigger_update` | 触发 Arkloop 版本更新 |

`trigger_update` 是唯一需要通过 Installer Bridge 执行的操作，不走普通 API 调用。Bridge 负责拉镜像、重启服务、写入更新状态。Platform Agent 只调用 Bridge 的 HTTP 接口，不持有宿主机权限。

### 5.2 平台核心配置（via /v1/admin/platform-settings）

| 工具 | 对应端点 |
|------|---------|
| `get_platform_settings` | `GET /v1/admin/platform-settings` |
| `set_platform_setting(key, value)` | `PATCH /v1/admin/platform-settings/{key}` |
| `configure_email(config)` | `PUT /v1/admin/email/config` |
| `test_email` | `POST /v1/admin/email/test` |
| `configure_smtp_provider(config)` | `POST /v1/admin/smtp-providers` |
| `configure_captcha(provider, config)` | `PATCH /v1/admin/platform-settings/captcha.*` |
| `configure_registration(config)` | `PATCH /v1/admin/platform-settings/auth.*` |
| `configure_gateway(config)` | `PUT /v1/admin/gateway-config` |
| `update_custom_styles(css)` | `PATCH /v1/admin/platform-settings/custom_css` |

`configure_email` 接受 SMTP 配置对象（host、port、user、password、from），配置完成后自动调用 `test_email` 验证连通性。

`configure_captcha` 目前支持 Cloudflare Turnstile，参数为 `site_key` 和 `secret_key`。

`update_custom_styles` 对应 CSS 注入功能。此工具依赖前端 CSS token 体系确立后才能发挥完整作用，v1 接受任意 CSS 字符串写入。

### 5.3 LLM Provider（via /v1/llm-providers）

| 工具 | 对应端点 |
|------|---------|
| `list_providers` | `GET /v1/llm-providers` |
| `add_provider(type, config)` | `POST /v1/llm-providers` |
| `update_provider(id, config)` | `PATCH /v1/llm-providers/{id}` |
| `delete_provider(id)` | `DELETE /v1/llm-providers/{id}` |
| `list_models(provider_id)` | `GET /v1/llm-providers/{id}/models` |
| `configure_model(provider_id, model_id, config)` | `PATCH /v1/llm-providers/{id}/models/{model_id}` |

Provider 的 credential（API Key）通过 `PermDataLLMCreds` 权限写入，Platform Agent 持有此权限但工具层不返回已存储的 key 内容，只返回脱敏摘要。

### 5.4 Agent（via /v1/personas）

| 工具 | 对应端点 |
|------|---------|
| `list_agents` | `GET /v1/personas` |
| `create_agent(config)` | `POST /v1/personas` |
| `update_agent(id, config)` | `PATCH /v1/personas/{id}` |
| `delete_agent(id)` | `DELETE /v1/personas/{id}` |
| `get_agent(id)` | `GET /v1/personas/{id}` |

`create_agent` 接受 `name`、`description`、`tool_allowlist`、`temperature`、`budget` 等参数，生成完整的 Persona 配置并持久化。

### 5.5 Skill（via /v1/skill-packages）

| 工具 | 对应端点 |
|------|---------|
| `list_skills` | `GET /v1/skill-packages` |
| `install_skill_from_market(skill_id)` | `POST /v1/market/skills/import` |
| `install_skill_from_github(url)` | `POST /v1/skill-packages/import/github` |
| `remove_skill(id)` | `DELETE /v1/skill-packages/{id}` |

写 Skill 代码不在 v1 工具集内。v1 只处理从市场或 GitHub 安装已有 Skill。

### 5.6 工具与 MCP（via /v1/mcp-configs, /v1/tool-providers）

| 工具 | 对应端点 |
|------|---------|
| `list_mcp_configs` | `GET /v1/mcp-configs` |
| `add_mcp_config(config)` | `POST /v1/mcp-configs` |
| `update_mcp_config(id, config)` | `PATCH /v1/mcp-configs/{id}` |
| `delete_mcp_config(id)` | `DELETE /v1/mcp-configs/{id}` |
| `list_tool_providers` | `GET /v1/tool-providers` |
| `add_tool_provider(config)` | `POST /v1/tool-providers` |
| `update_tool_provider(id, config)` | `PATCH /v1/tool-providers/{id}` |

### 5.7 社交 Channel（via /v1/channels，待实现）

此工具组依赖 Channel Integration Architecture 完成后才可实现。

| 工具 | 操作 |
|------|------|
| `list_channels` | 列出已配置的 Channel |
| `add_channel(type, config)` | 新增 Channel（Telegram / Discord / 飞书） |
| `update_channel(id, config)` | 更新 Channel 配置（Bot Token、Webhook URL 等） |
| `delete_channel(id)` | 删除 Channel |
| `bind_agent_to_channel(channel_id, persona_id)` | 绑定 Agent 到 Channel |

`config` 中包含各平台的 Bot Token 或 App Secret，通过 `PermDataSecrets` 写入。v1 暂不授予 `PermDataSecrets`，Channel 工具在 Platform Agent v2 引入。

### 5.8 访问控制（via /v1/ip-rules, /v1/admin/users）

| 工具 | 对应端点 |
|------|---------|
| `list_ip_rules` | `GET /v1/ip-rules` |
| `add_ip_rule(cidr, action)` | `POST /v1/ip-rules` |
| `delete_ip_rule(id)` | `DELETE /v1/ip-rules/{id}` |
| `list_api_keys` | `GET /v1/api-keys` |
| `create_api_key(name, scopes)` | `POST /v1/api-keys` |
| `revoke_api_key(id)` | `DELETE /v1/api-keys/{id}` |

用户邀请与角色管理（`/v1/admin/users`）v1 排除，避免 Platform Agent 影响用户账户安全边界。

---

## 6. Platform Persona 配置

### 6.1 persona.yaml

```yaml
id: platform
version: "1"
title: Platform
description: 平台管理 Agent，处理从基础设施配置到 Agent 创建的一切管理操作。
soul_file: soul.md
is_builtin: true
is_system: true
user_selectable: false
tool_allowlist:
  - get_platform_status
  - list_installed_modules
  - install_module
  - trigger_update
  - get_platform_settings
  - set_platform_setting
  - configure_email
  - test_email
  - configure_smtp_provider
  - configure_captcha
  - configure_registration
  - configure_gateway
  - update_custom_styles
  - list_providers
  - add_provider
  - update_provider
  - delete_provider
  - list_models
  - configure_model
  - list_agents
  - create_agent
  - update_agent
  - delete_agent
  - list_skills
  - install_skill_from_market
  - install_skill_from_github
  - remove_skill
  - list_mcp_configs
  - add_mcp_config
  - update_mcp_config
  - delete_mcp_config
  - list_tool_providers
  - add_tool_provider
  - update_tool_provider
  - list_ip_rules
  - add_ip_rule
  - delete_ip_rule
  - list_api_keys
  - create_api_key
  - revoke_api_key
budgets:
  reasoning_iterations: 20
  max_output_tokens: 4096
  temperature: 0.3
```

`is_system: true` 的 Persona 不可被用户删除、修改 tool_allowlist 或直接 spawn（只能通过 `call_platform` 工具触发）。

`temperature: 0.3` 是有意为之——管理操作要求确定性，不需要创造性输出。

### 6.2 soul.md 结构

Platform Agent 的 `soul.md` 需要覆盖以下约束，具体文案待写：

- 操作前必须确认：破坏性操作（delete、revoke、trigger_update）执行前必须向用户复述将要做什么并等待确认
- 敏感值处理：API Key、SMTP 密码等敏感值在确认信息中脱敏显示
- 范围声明：在每次回复开头简短说明本次将要操作的范围
- Styles 上下文：CSS token 体系确立后补入（当前为 placeholder）
- 不自我修改：不执行任何影响 Platform Persona 自身配置的操作

---

## 7. `call_platform` 工具设计

顶层 Agent 通过此工具委托 Platform Agent：

```json
{
  "name": "call_platform",
  "description": "委托平台管理 Agent 执行管理操作。用于配置邮件、添加模型 Provider、创建 Agent、安装 Skill、配置社交 Channel 等所有平台管理任务。",
  "schema": {
    "type": "object",
    "properties": {
      "task": {
        "type": "string",
        "description": "要执行的管理任务描述"
      }
    },
    "required": ["task"]
  }
}
```

内部实现：`SubAgentControl.Spawn(platform, isolated, task)` + `Wait` + `Collect`，返回 Platform Agent 的最终输出。

此工具只在以下条件下注入顶层 Agent 的工具面：

- 当前用户持有 `org_admin` 或 `platform_admin` 角色
- 顶层 Persona 配置中 `allow_platform_delegation: true`（默认 false，需显式开启）

---

## 8. `trigger_update` 的安全处理

`trigger_update` 是工具集中唯一具有宿主级副作用的操作，单独处理：

- 不直接执行 shell，调用 Installer Bridge 的 `POST /bridge/update` 接口
- Bridge 负责：拉取新镜像、健康检查、滚动重启、回写更新状态
- Platform Agent 只获得更新是否成功的结果，不获得执行过程的原始输出
- 调用前强制要求用户确认，soul.md 中硬性约束，不可被用户 prompt 覆盖
- 失败时 Bridge 自动回滚，Platform Agent 报告失败原因

---

## 9. 工具实现位置

Platform Agent 工具实现放在 worker 的独立目录：

```
src/services/worker/internal/tools/builtin/platform/
  executor_provider.go
  executor_persona.go
  executor_skill.go
  executor_settings.go
  executor_mcp.go
  executor_infra.go
  executor_access.go
  spec.go
```

每个工具通过内部 HTTP client 调用 API service，使用 `system_agent` 的 service token 鉴权，不走外部网络。

---

## 10. 依赖关系

| 依赖 | 状态 | 说明 |
|------|------|------|
| Sub-agent 架构 Track A/B | 待实现 | Platform Agent 需要 Control Plane 的 identity 注入能力 |
| Installer Bridge | 待实现 | `trigger_update` 和 `install_module` 依赖 Bridge HTTP 接口 |
| Channel Integration | 待实现 | 社交 Channel 工具组依赖 Channel 架构完成 |
| CSS Token 体系 | 待定义 | `update_custom_styles` 工具的 soul.md 上下文依赖此体系 |

在 Sub-agent Track A/B 完成前，Platform Agent 可以作为独立 run 先行实现（复用现有 `spawnChildRun`），identity 注入改造在 Track B 完成后统一接入。

---

## 11. 成功标准

以下条件全部满足时，Platform Agent v1 可视为可用：

- 用户可通过自然语言完成：添加 LLM Provider、创建 Agent、安装 Skill、配置 SMTP、配置 Captcha
- 破坏性操作（delete、revoke、update）执行前有明确确认步骤
- Platform Agent 不可被普通用户直接 spawn
- `system_agent` 身份与调用者身份完全隔离
- `trigger_update` 通过 Bridge 执行，不暴露宿主机访问
- 所有操作产生审计日志

---

## 12. 实施路线图

以 PR 为粒度拆分，每个 PR 可独立合并、独立验证，不依赖后续 PR 才能运行。

---

### PR-1：RoleSystemAgent 与 system_agent 身份

**改动范围**

- `src/services/api/internal/auth/roles.go`：新增 `RoleSystemAgent` 常量与 `systemAgentPerms` 权限集
- `src/services/api/internal/auth/permissions.go`：无需改动（复用现有权限常量）
- 新增 migration：seed `system_agent` user 与 org membership

**验收**

- `PermissionsForRole("system_agent")` 返回正确权限集
- system_agent user 存在于 DB，不可登录，不可删除
- 现有角色测试全部通过

---

### PR-2：Platform tools — 工具框架与 settings/email 组

**改动范围**

- 新建 `src/services/worker/internal/tools/builtin/platform/`
- `spec.go`：所有 Platform 工具的 `AgentToolSpec` 与 `LlmSpec` 定义
- `executor_settings.go`：`get_platform_settings`、`set_platform_setting`、`configure_email`、`test_email`、`configure_smtp_provider`、`configure_captcha`、`configure_registration`、`configure_gateway`、`update_custom_styles`
- 工具内部使用 internal HTTP client 调用 API service，token 从 worker 启动时注入

**验收**

- 单测：每个工具的参数校验与 error mapping
- `configure_email` 调用后 `test_email` 可验证连通性

---

### PR-3：Platform tools — provider / persona / skill / mcp / access 组

**改动范围**

- `executor_provider.go`：`list_providers`、`add_provider`、`update_provider`、`delete_provider`、`list_models`、`configure_model`
- `executor_persona.go`：`list_agents`、`create_agent`、`update_agent`、`delete_agent`、`get_agent`
- `executor_skill.go`：`list_skills`、`install_skill_from_market`、`install_skill_from_github`、`remove_skill`
- `executor_mcp.go`：`list_mcp_configs`、`add_mcp_config`、`update_mcp_config`、`delete_mcp_config`、`list_tool_providers`、`add_tool_provider`、`update_tool_provider`
- `executor_access.go`：`list_ip_rules`、`add_ip_rule`、`delete_ip_rule`、`list_api_keys`、`create_api_key`、`revoke_api_key`

**验收**

- 单测：每组工具的参数校验
- `create_agent` 产生的 Persona 可被正常 resolve

---

### PR-4：Platform tools — infra 组（依赖 Installer Bridge）

**改动范围**

- `executor_infra.go`：`get_platform_status`、`list_installed_modules`、`install_module`、`trigger_update`
- `trigger_update` 调用 Bridge `POST /bridge/update`，需要 Bridge HTTP 接口已实现

**依赖**

- Installer Bridge PR 先合并

**验收**

- `get_platform_status` 在 Bridge 未部署时返回降级响应，不 panic
- `trigger_update` 在 Bridge 返回失败时正确上报错误

---

### PR-5：Platform Persona 定义

**改动范围**

- 新建 `src/personas/platform/persona.yaml`：`is_builtin: true`、`is_system: true`、完整 `tool_allowlist`
- 新建 `src/personas/platform/prompt.md`
- 新建 `src/personas/platform/soul.md`：确认机制、敏感值脱敏、范围声明、禁止自我修改
- Persona loader：识别 `is_system: true`，阻止用户删除与 tool_allowlist 修改

**验收**

- Platform Persona 可被加载与 resolve
- 普通用户 API 调用无法删除或修改 Platform Persona
- `user_selectable: false` 使其不出现在前端 Persona 选择器

---

### PR-6：`call_platform` 工具注入

**改动范围**

- `src/services/worker/internal/tools/builtin/builtin.go`：注册 `call_platform` 工具
- `src/services/worker/internal/pipeline/mw_tool_build.go`：在以下条件同时满足时注入 `call_platform`：actor 持有 `org_admin` 或 `platform_admin` 角色，且当前 Persona 的 `allow_platform_delegation: true`
- `persona.yaml` schema：新增 `allow_platform_delegation` 布尔字段

**验收**

- 普通用户的 agent loop 中不出现 `call_platform`
- admin 用户 + 开启 delegation 的 Persona 中工具面包含 `call_platform`

---

### PR-7：identity 注入（依赖 Sub-agent Track B）

**改动范围**

- `src/services/worker/internal/runengine/child_run.go`（或新 Control Plane）：当 `source_type == platform_agent` 时，替换 `CreatedByUserID` 为 `system_agent` UUID
- `call_platform` executor：spawn 时写入 `source_type: platform_agent`

**依赖**

- Sub-agent 架构 Track B（Control Plane）合并后接入

**过渡方案**

- PR-6 合并后，`call_platform` 临时复用 `spawnChildRun`，identity 注入在本 PR 补齐
- 过渡期内 Platform Agent 以调用者身份运行，权限受调用者限制（功能受限但不阻塞开发）

**验收**

- Platform Agent run 的 `created_by_user_id` 为 `system_agent` UUID
- 父 run 的普通用户身份无法访问 Platform 工具

---

### PR-8：Channel 工具组（依赖 Channel Integration 架构完成）

**改动范围**

- `executor_channel.go`：`list_channels`、`add_channel`、`update_channel`、`delete_channel`、`bind_agent_to_channel`
- `roles.go`：`systemAgentPerms` 追加 `PermDataSecrets`
- Platform Persona `tool_allowlist` 追加 channel 工具

**依赖**

- Channel Integration 架构 PR 合并
- 安全评审：`PermDataSecrets` 授权边界确认

**验收**

- Telegram / Discord Bot Token 可通过 Platform Agent 写入
- Token 在工具响应中脱敏
