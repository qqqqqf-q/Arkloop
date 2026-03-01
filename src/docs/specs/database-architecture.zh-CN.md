# 数据库架构与数据模型

本文描述 Arkloop 的数据库边界、核心表与权限/审计/计费的架构约束。生产形态以 PostgreSQL 为唯一目标后端。

迁移工具：Goose（嵌入 `src/services/api/internal/migrate/migrations/`，74 个迁移文件）。

## 1. 术语

`org` 是**租户边界（tenant boundary）**：
- 数据隔离边界（权限、导出、删除、保留策略）
- 审计边界（日志归属与追责范围）
- 计费与配额边界（预算、倍率、用量报表）

## 2. 顶层结构：`org / team / project`

### 2.1 `orgs`（租户/公司）

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

### 2.3 `org_memberships`（组织成员关系）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `user_id` | FK -> users |
| `role` | 角色（owner / member） |

### 2.4 `teams`（组织内小组）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `name` | 名称 |

### 2.5 `projects`（项目/协作域）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
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
| `org_id` | FK -> orgs |
| `created_by_user_id` | FK -> users |
| `title` | 标题 |
| `project_id` | FK -> projects（可选） |
| `agent_config_id` | FK -> agent_configs（可选） |
| `private` | 私有标记 |
| `deleted_at` | 软删除 |
| `created_at` | 创建时间 |

### 3.2 `messages`（消息）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `thread_id` | FK -> threads |
| `org_id` | FK -> orgs |
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
| `org_id` | FK -> orgs |
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

## 5. LLM 凭证与路由

### 5.1 `llm_credentials`（LLM 提供商凭证）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs（可选，platform 级别为 NULL） |
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
| `org_id` | FK -> orgs |
| `credential_id` | FK -> llm_credentials |
| `model` | 模型标识 |
| `priority` | 优先级 |
| `is_default` | 默认路由标记 |
| `when_json` | JSONB 条件规则 |
| `multiplier` | 费率倍率 |
| `cache_pricing_json` | 缓存定价 |

### 5.3 `secrets`（通用加密存储）

AES-256-GCM 加密，密钥由 `ARKLOOP_ENCRYPTION_KEY` 提供。

## 6. Personas 与 Agent 配置

### 6.1 `personas`（人格定义）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `persona_key` | 人格标识 |
| `version` | 版本 |
| `display_name` | 显示名称 |
| `description` | 描述 |
| `prompt_md` | system prompt |
| `tool_allowlist` | 允许的工具列表 |
| `tool_denylist` | 禁止的工具列表 |
| `preferred_credential` | 首选凭证 |
| `agent_config_name` | 关联 agent 配置 |

### 6.2 `agent_configs`（Agent 配置）

| 列 | 说明 |
|----|------|
| `id` | PK |
| `org_id` | FK -> orgs |
| `name` | 配置名称 |
| `system_prompt_override` | system prompt 覆盖 |
| `model` | 模型标识 |
| `temperature` | 温度 |
| `max_output_tokens` | 最大输出 token |
| `tool_policy` | 工具策略 |
| `tool_allowlist` | 工具白名单 |
| `cache_control_json` | 缓存控制 |
| `reasoning_mode` | 推理模式 |
| `scope` | 作用域 |

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
| `org_id` | FK -> orgs |
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
| `org_id` | FK -> orgs |
| `key` | 功能键 |
| `value` | 覆盖值 |
| `reason` | 原因 |
| `expires_at` | 过期时间 |

### 7.5 `credits` / `credit_transactions`（积分体系）

| 表 | 关键列 |
|----|--------|
| `credits` | org_id, amount, balance |
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
| `org_id` | FK -> orgs |
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
| `org_id` | FK -> orgs |
| `name` | 服务器名称 |
| `url` | 连接地址 |
| `env_json` | 环境变量 |
| `tools_json` | 工具定义 |

### 12.2 `asr_credentials`（语音转文字凭证）

与 `llm_credentials` 结构类似，独立管理。

## 13. 其他

| 表 | 说明 |
|----|------|
| `user_memory_snapshots` | 用户记忆快照（org_id, data_json, hits_json），对接 OpenViking |
| `platform_settings` | 全局平台配置（key-value JSONB） |
| `feature_flags` | 功能开关 |
| `redemption_codes` | 兑换码（value, usage_count, expires_at） |
| `invite_codes` | 邀请码 |

## 14. 架构决策记录

- **存储引擎**：PostgreSQL（唯一生产后端）
- **加密**：AES-256-GCM（`ARKLOOP_ENCRYPTION_KEY`），用于 `llm_credentials`、`asr_credentials`、`secrets`
- **分区**：`run_events` 按月分区（`created_at`），自动清理过期分区
- **软删除**：`threads`、`notification_broadcasts`、`projects` 使用 `deleted_at`
- **UUID**：主键使用 UUID（`pgcrypto` 扩展）
- **任务队列**：PostgreSQL 表 + Advisory Lock（不依赖外部 MQ）
- **实时推送**：PostgreSQL `LISTEN/NOTIFY` -> SSE
- **凭证范围**：`llm_credentials` 支持 platform 级（`org_id` 为 NULL）和 org 级两种作用域
