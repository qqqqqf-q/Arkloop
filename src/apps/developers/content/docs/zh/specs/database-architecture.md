---
title: "数据库架构与数据模型"
---
本文描述 Arkloop 的数据库边界、核心表与权限/审计/计费的架构约束。生产形态以 PostgreSQL 为唯一目标后端。

迁移工具：Goose（嵌入 `src/services/api/internal/migrate/migrations/`，139 个迁移文件）。

**注**：Organization 概念已通过 migration 00118 移除。Account 是唯一的租户单元。

## 1. 术语

`account` 是**租户边界（tenant boundary）**：
- 数据隔离边界（权限、导出、删除、保留策略）
- 审计边界（日志归属与追责范围）
- 计费与配额边界（预算、倍率、用量报表）

`platform` 是**部署实例的全局作用域**：
- 平台级默认配置与平台级凭证（用于“新 org 无配置也可运行”）
- 由 `platform_admin` 管理，不属于任何 org
- org 级配置只做覆盖，不应承担“全局默认”的职责

## 2. 顶层结构：`org / team / project`

### 2.1 `accounts`（租户/公司）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `slug` | URL 友好标识 |
| `name` | 显示名称 |
| `created_at` | 创建时间 |

### 2.2 `users`（用户主体）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `username` | 用户名 |
| `created_at` | 创建时间 |

### 2.3 `account_memberships`（组织成员关系）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `user_id` | FK -> users |
| `role` | 角色（owner / member） |

### 2.4 `teams`（组织内小组）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `name` | 名称 |

### 2.5 `projects`（项目/协作域）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `team_id` | FK -> teams（可选） |
| `name` | 名称 |
| `description` | 描述 |
| `visibility` | 可见性 |
| `deleted_at` | 软删除标记 |

## 3. 会话与消息

### 3.1 `threads`（会话容器）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `created_by_user_id` | FK -> users |
| `title` | 标题 |
| `project_id` | FK -> projects（可选） |
| `private` | 私有标记 |
| `deleted_at` | 软删除 |
| `created_at` | 创建时间 |

### 3.2 `messages`（消息）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `thread_id` | FK -> threads |
| `account_id` | FK -> accounts |
| `role` | user / assistant / system |
| `content` | 文本内容 |
| `content_json` | JSONB 结构化内容 |
| `hidden` | 隐藏标记 |
| `created_at` | 创建时间 |

## 4. 运行与事件

### 4.1 `runs`（执行实例）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `thread_id` | FK -> threads |
| `created_by_user_id` | FK -> users |
| `status` | 状态机 |
| `parent_run_id` | FK -> runs（子运行） |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |

### 4.2 `run_events`（事件流 -- 唯一真相）

**按月分区**（`created_at`），自动管理分区生命周期（`ARKLOOP_RUN_EVENTS_RETENTION_MONTHS`）。

| 列 | 说明 |
|----|------|
| `event_id` | PK |
| `run_id` | FK -> runs |
| `seq` | run 内单调递增序号 |
| `ts` | 服务端时间戳 |
| `type` | 事件类型 |
| `data_json` | JSONB 负载 |
| `tool_name` | 抽列索引 |
| `error_class` | 抽列索引 |
| `created_at` | 分区键 |

关键约束：
- `seq` 在同一 run 内严格递增
- Worker 写入，API 读取并以 SSE 回放
- 支持 `after_seq` 游标断线续传

## 5. LLM Providers 与路由

### 5.1 Provider 账号

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts（当前为 org 级资源） |
| `provider` | 提供商标识 |
| `name` | 显示名称 |
| `secret_id` | FK -> secrets（加密存储） |
| `key_prefix` | 密钥前缀（用于识别） |
| `base_url` | 自定义 base URL |
| `advanced_json` | JSONB 高级配置 |

### 5.2 `llm_routes`（模型路由规则）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `credential_id` | FK -> Provider 账号记录 |
| `model` | 模型标识 |
| `priority` | 优先级 |
| `is_default` | 默认路由标记 |
| `when_json` | JSONB 条件规则 |
| `multiplier` | 费率倍率 |
| `cache_pricing_json` | 缓存定价 |

### 5.3 `secrets`（通用加密存储）

AES-256-GCM 加密，密钥由 `ARKLOOP_ENCRYPTION_KEY` 提供。

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts（org scope 必填；platform scope 为 NULL） |
| `scope` | `org` / `platform` |
| `name` | 逻辑键（同 scope 内唯一） |
| `encrypted_value` | 密文（base64） |
| `key_version` | 加密版本 |
| `rotated_at` | 轮换时间（可选） |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |

约束：
- `scope='org'`：`(account_id, name)` 唯一
- `scope='platform'`：`name` 全局唯一

`secrets` 用途：
- LLM / ASR 等凭证的 API Key
- Tool Providers 的 API Key

当前：Config Registry 标记为 `Sensitive=true` 的配置项，在 API 返回时会被 mask；值仍写入 `platform_settings/org_settings`（不加密）。

### 5.4 `platform_settings` / `org_settings`（统一配置：Config Resolver）

用于 Track A 的 Config Resolver（key-value 配置），支持平台默认 + org 覆盖。

#### `platform_settings`

| 列 | 说明 |
|----|------|
| `key` | PK |
| `value` | 配置值（非敏感） |
| `updated_at` | 更新时间 |

#### `org_settings`

| 列 | 说明 |
|----|------|
| `account_id` | FK -> accounts |
| `key` | 配置键 |
| `value` | 配置值（非敏感） |
| `updated_at` | 更新时间 |

Resolver 的优先级链（从高到低）：
1) ENV override（部署层强制覆盖）
2) `org_settings`
3) `platform_settings`
4) Registry 默认值

### 5.5 `tool_provider_configs`（工具后端激活与凭证关联）

用于 `web_search` / `web_fetch` 等 Tool Group 的后端选择、凭证与 base_url 配置。

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts（org scope 必填；platform scope 为 NULL） |
| `scope` | `org` / `platform` |
| `group_name` | Tool Group 名（LLM 看到的工具名，如 `web_search`） |
| `provider_name` | Provider 名（内部工具名，如 `web_search.tavily`） |
| `is_active` | 是否激活（每 scope + group 最多一个 active） |
| `secret_id` | FK -> secrets（API Key，加密存储） |
| `key_prefix` | 密钥前缀（用于 Console 展示） |
| `base_url` | 自定义 endpoint（SearXNG / 自部署 Firecrawl 等） |
| `config_json` | 非敏感参数（JSONB） |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |

解析链：
- org scope active provider 优先
- 无 org 配置时回落 platform scope active provider

## 6. Personas

### 6.1 `personas`（人格定义）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `persona_key` | 人格标识 |
| `version` | 版本 |
| `display_name` | 显示名称 |
| `description` | 描述 |
| `prompt_md` | system prompt |
| `tool_allowlist` | 允许的工具列表 |
| `tool_denylist` | 禁止的工具列表 |
| `budgets_json` | 温度、输出上限、工具预算等运行预算 |
| `model` | 可选 model selector，格式为 `provider_name^model_name` 或裸 `model` |
| `reasoning_mode` | 推理模式 |
| `stream_thinking` | 是否向客户端下发 `message.delta` 的 `channel: thinking`（默认 true；省略 persona YAML 键时视为 true） |
| `prompt_cache_control` | prompt cache 控制策略 |
| `preferred_credential` | 当 `model` 为空时的后备 Provider 名称 |

当前 Persona 已吸收原本的 Agent Config / Prompt Template 执行配置，不再维护独立表层。
## 7. 计费与配额

### 7.1 `plans`（订阅计划）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `name` | 计划标识 |
| `display_name` | 显示名称 |

### 7.2 `subscriptions`（订阅关系）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `plan_id` | FK -> plans |
| `status` | 状态 |
| `current_period_start` | 当前周期起 |
| `current_period_end` | 当前周期止 |
| `cancelled_at` | 取消时间 |

### 7.3 `plan_entitlements`（计划功能配额）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `plan_id` | FK -> plans |
| `key` | 功能键 |
| `value` | 配额值 |
| `value_type` | 值类型 |

### 7.4 `org_entitlement_overrides`（组织级覆盖）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `key` | 功能键 |
| `value` | 覆盖值 |
| `reason` | 原因 |
| `expires_at` | 过期时间 |

### 7.5 `credits` / `credit_transactions`（积分体系）

| 表 | 关键列 |
|----|--------|
| `credits` | account_id, amount, balance |
| `credit_transactions` | credits_id, amount, type |

### 7.6 `usage_records`（用量记录）

缓存列：`input_tokens`、`output_tokens`、`cache_hit_rate`。

## 8. 社交与分享

| 表 | 说明 |
|----|------|
| `thread_stars` | 收藏（thread_id + user_id） |
| `thread_shares` | 分享（shared_by_user_id, recipient_user_id） |
| `thread_reports` | 举报（reason, status） |

## 9. 基础设施

### 9.1 `jobs`（后台任务队列）

PostgreSQL 表 + Advisory Lock 实现的任务队列。

| 列 | 说明 |
|----|------|
| `id` | PK |
| `job_type` | 类型（`run.execute` / `webhook.deliver` / `email.send`） |
| `payload_json` | JSONB 负载（跨语言协议，必须版本化） |
| `status` | 状态 |
| `available_at` | 可用时间 |
| `leased_until` | 租约到期 |
| `attempts` | 重试次数 |
| `worker_tags` | Worker 能力标签 |

### 9.2 `worker_registrations`（Worker 注册）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `name` | Worker 名称 |
| `capabilities_json` | 能力集 |
| `heartbeat_at` | 心跳时间 |

### 9.3 `webhook_endpoints`（Webhook）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `url` | 回调地址 |
| `events` | 订阅事件类型数组 |
| `active` | 启用状态 |

### 9.4 `api_keys`（API 密钥）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `key_prefix` | 密钥前缀 |
| `last_used_at` | 最后使用时间 |

## 10. 认证与安全

| 表 | 说明 |
|----|------|
| `user_credentials` | 登录凭证（login, password_hash） |
| `refresh_tokens` | JWT refresh token（user_id, token, revoked_at） |
| `email_verification_tokens` | 邮箱验证 |
| `email_otp_tokens` | OTP（email, code, expires_at） |
| `rbac_roles` | 角色定义（permissions_json） |

## 11. 通知与审计

| 表 | 说明 |
|----|------|
| `notifications` | 用户通知（type, title, body, read_at） |
| `notification_broadcasts` | 平台广播（软删除） |
| `audit_logs` | 审计日志（user_id, action, resource_type, ip_address, user_agent） |

## 12. MCP 与外部集成

### 12.1 `mcp_configs`（MCP 服务器配置）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `account_id` | FK -> accounts |
| `name` | 服务器名称 |
| `url` | 连接地址 |
| `env_json` | 环境变量 |
| `tools_json` | 工具定义 |

### 12.2 `asr_credentials`（语音转文字凭证）

与 Provider 密钥存储结构类似，独立管理。

## 13. 其他

| 表 | 说明 |
|----|------|
| `user_memory_snapshots` | 用户记忆快照（account_id, data_json, hits_json），对接 OpenViking |
| `platform_settings` | 全局平台配置（key-value JSONB） |
| `feature_flags` | 功能开关 |
| `redemption_codes` | 兑换码（value, usage_count, expires_at） |
| `invite_codes` | 邀请码 |

## 14. 架构决策记录

- **存储引擎**：PostgreSQL（唯一生产后端）
- **加密**：AES-256-GCM（`ARKLOOP_ENCRYPTION_KEY`），用于 LLM Provider 密钥、`asr_credentials`、`secrets`
- **分区**：`run_events` 按月分区（`created_at`），自动清理过期分区
- **软删除**：`threads`、`notification_broadcasts`、`projects` 使用 `deleted_at`
- **UUID**：主键使用 UUID（`pgcrypto` 扩展）
- **任务队列**：PostgreSQL 表 + Advisory Lock（不依赖外部 MQ）
- **实时推送**：PostgreSQL `LISTEN/NOTIFY` -> SSE
- **凭证范围**：LLM Providers 支持 platform 级（`account_id` 为 NULL）和 org 级两种作用域
