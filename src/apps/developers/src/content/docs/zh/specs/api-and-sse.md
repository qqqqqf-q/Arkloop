---
---

# 后端 API 与 SSE 规范

本文描述 Arkloop API 层的资源模型、端点设计、错误模型与 SSE 事件规范。API 使用 Go 实现，基于 `net/http` 标准库。

## 1. 核心原则

- **API 只做控制面**：鉴权、资源编排、审计落库、SSE 回放、enqueue job。工具执行在 Worker。
- **事件是唯一真相**：运行过程通过 `run_events` 表达，SSE 推送 + 落库 + 回放基于同一事件源。
- **流式优先**：模型输出与工具调用过程统一通过 SSE 推送。
- **多租户隔离**：所有写操作归属 `org_id`；数据可见性由 RBAC + 成员关系控制。
- **Fetch 流式鉴权**：SSE 与普通 API 使用同一 `Authorization: Bearer` 机制，Web/CLI 统一。

## 2. 资源模型

### 核心资源

| 资源 | 说明 |
|------|------|
| `orgs` | 租户边界（数据隔离、计费、审计） |
| `users` | 用户主体 |
| `org_memberships` | 组织成员关系与角色 |
| `teams` | 组织内小组 |
| `projects` | 项目/协作域（可关联 team） |
| `threads` | 会话容器（支持软删除） |
| `messages` | 用户/assistant 消息（`content_json` JSONB） |
| `runs` | Agent Loop 执行实例（支持 `parent_run_id` 子运行） |
| `run_events` | 事件流（按月分区，`seq` 单调递增） |

### 配置资源

| 资源 | 说明 |
|------|------|
| `llm_providers` | 提供商账号与其下的模型列表，用于构造 model selector |
| `llm_routes` | 模型路由规则（provider model + priority + multiplier） |
| `asr_credentials` | 语音转文字凭证 |
| `mcp_configs` | MCP 服务器配置（stdio/HTTP 类型） |
| `personas` | 人格定义（prompt + 工具策略 + budgets + model selector） |

### 企业资源

| 资源 | 说明 |
|------|------|
| `api_keys` | 外部访问密钥（哈希存储） |
| `ip_rules` | IP 访问规则 |
| `webhook_endpoints` | Webhook 端点 |
| `plans` / `subscriptions` | 订阅计划体系 |
| `credits` / `credit_transactions` | 积分/额度体系 |
| `entitlement_overrides` | 组织级功能权限覆盖 |
| `audit_logs` | 审计日志 |
| `notifications` / `notification_broadcasts` | 通知体系 |
| `user_memory_snapshots` | 用户记忆快照（OpenViking） |

## 3. 端点设计

### 3.1 健康检查

- `GET /healthz` -- 存活探针
- `GET /readyz` -- 就绪探针（含 schema 版本校验）

### 3.2 认证与会话

- `POST /v1/auth/register` -- 注册
- `POST /v1/auth/login` -- 登录
- `POST /v1/auth/refresh` -- 刷新 token（基于 HttpOnly cookie）
- `POST /v1/auth/logout` -- 登出
- `POST /v1/auth/resolve` -- 解析认证下一步
- `GET /v1/auth/registration-mode` -- 注册模式查询
- `GET /v1/auth/captcha-config` -- Captcha 配置（Cloudflare Turnstile）
- `POST /v1/auth/email/verify/send` -- 发送邮箱验证
- `POST /v1/auth/email/verify/confirm` -- 确认邮箱验证
- `POST /v1/auth/email/otp/send` -- 发送 OTP
- `POST /v1/auth/email/otp/verify` -- 验证 OTP
- `POST /v1/auth/resolve/otp/send` -- 为已解析身份发送 OTP
- `POST /v1/auth/resolve/otp/verify` -- 为已解析身份验证 OTP

### 3.3 用户

- `GET /v1/me` -- 当前用户信息
- `GET /v1/me/usage` -- 用量统计
- `GET /v1/me/usage/daily` -- 每日用量
- `GET /v1/me/usage/by-model` -- 按模型用量
- `GET /v1/me/feedback` -- 反馈记录
- `POST /v1/me/credits` -- 积分操作
- `GET /v1/me/invite-code` -- 邀请码
- `POST /v1/me/invite-code/reset` -- 重置邀请码
- `POST /v1/me/redeem` -- 兑换码核销

### 3.4 会话与消息

- `GET /v1/threads` -- 列表
- `POST /v1/threads` -- 创建（可选 `project_id`）
- `GET /v1/threads/{id}` -- 详情
- `PUT /v1/threads/{id}` -- 更新
- `DELETE /v1/threads/{id}` -- 软删除
- `GET /v1/threads/search` -- 搜索
- `GET /v1/threads/starred` -- 收藏列表
- `GET /v1/threads/{id}/messages` -- 消息列表
- `POST /v1/threads/{id}/messages` -- 写入用户消息

### 3.5 运行（Run）

Run 把「输入消息 + Persona 配置 + 路由策略」变成一次可审计的执行链路。

- `POST /v1/threads/{id}/runs` -- 创建 run
  - 入参：`persona_id`（可选）、`route_id`（可选）、配置覆盖
  - API 同一事务内：写 `runs` 行 + 写 `run.started` 事件 + 插入 `jobs`（`run.execute`）
- `GET /v1/runs` -- 列表
- `GET /v1/runs/{id}` -- SSE 事件流（`Content-Type: text/event-stream`）
- `POST /v1/runs/{id}/cancel` -- 取消
- `POST /v1/runs/{id}/input` -- 提交用户输入（Human-in-the-loop）
- `POST /v1/runs/{id}/retry` -- 重试

SSE 约定：
- 事件按 `seq` 单调递增
- 支持 `?after_seq=N` 游标断线续传
- 心跳间隔：15s（`ARKLOOP_SSE_HEARTBEAT_SECONDS`）
- 批次上限：500（`ARKLOOP_SSE_BATCH_LIMIT`）
- 传输层：PostgreSQL `LISTEN/NOTIFY`（通过 `ARKLOOP_DATABASE_DIRECT_URL` 直连，绕过 PgBouncer）

### 3.6 公开分享

- `GET /v1/s/{share_id}` -- 公开分享访问

### 3.7 LLM Providers 与模型

- `GET/POST /v1/llm-providers` -- Provider 账号管理
- `PATCH/DELETE /v1/llm-providers/{id}`
- `POST /v1/llm-providers/{id}/models` -- 创建 Provider 模型
- `PATCH/DELETE /v1/llm-providers/{id}/models/{model_id}`
- `GET /v1/llm-providers/{id}/available-models` -- 查询上游模型目录

### 3.8 ASR（语音转文字）

- `GET/POST /v1/asr-credentials`
- `GET/PUT/DELETE /v1/asr-credentials/{id}`
- `POST /v1/asr/transcribe` -- 转写

### 3.9 MCP 配置

- `GET/POST /v1/mcp-configs`
- `GET/PUT/DELETE /v1/mcp-configs/{id}`

### 3.10 Personas

说明：对外命名已从 `skills` 迁移为 `personas`（`/v1/skills` -> `/v1/personas`，`skill_key/skill_id` -> `persona_key/persona_id`）。执行配置直接收敛到 Persona，因此不再存在独立的 Agent Config / Prompt Template 层。

补充：Persona 管理接口支持可选 `roles` 对象。`roles.<role>` 可覆盖协作角色的 prompt 追加段、工具策略、budgets、`model`、`preferred_credential`、`reasoning_mode`、`prompt_cache_control`，但不允许覆盖执行器类型与执行器配置。

- `GET /v1/me/selectable-personas` -- 当前用户可选的人格有效结果，按 `org > platform > builtin` 解析
- `GET /v1/personas`
- `POST /v1/personas`
- `PATCH /v1/personas/{id}`

说明：`/v1/personas` 为原始管理接口；`/v1/me/selectable-personas` 为运行时有效人格接口。

### 3.11 组织与团队

- `GET/POST /v1/orgs`
- `GET /v1/orgs/me` -- 当前用户所属组织
- `GET/POST /v1/orgs/{id}`
- `GET/POST /v1/orgs/{id}/invitations` -- 邀请管理
- `GET /v1/orgs/{id}/usage` -- 组织用量
- `GET /v1/orgs/{id}/usage/daily`
- `GET /v1/orgs/{id}/usage/by-model`
- `GET/POST /v1/org-invitations`
- `GET/PUT/DELETE /v1/org-invitations/{id}`
- `GET/POST /v1/teams`
- `GET/PUT/DELETE /v1/teams/{id}`
- `GET/POST /v1/projects`
- `GET/PUT/DELETE /v1/projects/{id}`

### 3.12 安全与访问控制

- `GET/POST /v1/api-keys`
- `GET/PUT/DELETE /v1/api-keys/{id}`
- `GET/POST /v1/ip-rules`
- `GET/PUT/DELETE /v1/ip-rules/{id}`

### 3.13 Webhooks

- `GET/POST /v1/webhook-endpoints`
- `GET/PUT/DELETE /v1/webhook-endpoints/{id}`

### 3.14 订阅与计费

- `GET/POST /v1/plans`
- `GET/PUT/DELETE /v1/plans/{id}`
- `GET/POST /v1/subscriptions`
- `GET/PUT/DELETE /v1/subscriptions/{id}`
- `GET/POST /v1/entitlement-overrides`
- `GET/PUT/DELETE /v1/entitlement-overrides/{id}`

### 3.15 通知与审计

- `GET/POST /v1/notifications`
- `GET/PUT/DELETE /v1/notifications/{id}`
- `GET /v1/audit-logs`
- `GET /v1/feature-flags`
- `GET/PUT/DELETE /v1/feature-flags/{id}`

### 3.16 Artifacts

- `GET/PUT/DELETE /v1/artifacts/{id}`

### 3.17 管理后台

管理后台端点前缀 `/v1/admin/`，需要平台管理员权限。

**仪表盘与报表：**
- `GET /v1/admin/dashboard`
- `GET /v1/admin/runs/{id}`
- `GET /v1/admin/reports`
- `GET /v1/admin/usage/daily`
- `GET /v1/admin/usage/summary`
- `GET /v1/admin/usage/by-model`
- `GET /v1/admin/access-log`

**用户管理：**
- `GET/POST /v1/admin/users`
- `GET/PUT/DELETE /v1/admin/users/{id}`

**邀请码：**
- `GET/POST /v1/admin/invite-codes`
- `GET/PUT/DELETE /v1/admin/invite-codes/{id}`

**推荐体系：**
- `GET /v1/admin/referrals`
- `GET /v1/admin/referrals/tree`

**积分管理：**
- `GET/POST /v1/admin/credits`
- `POST /v1/admin/credits/adjust`
- `POST /v1/admin/credits/bulk-adjust`
- `POST /v1/admin/credits/reset-all`

**兑换码：**
- `GET/POST /v1/admin/redemption-codes`
- `GET/PUT/DELETE /v1/admin/redemption-codes/{id}`
- `POST /v1/admin/redemption-codes/batch`

**通知广播：**
- `GET/POST /v1/admin/notifications/broadcasts`
- `GET/PUT/DELETE /v1/admin/notifications/broadcasts/{id}`

**平台配置：**
- `GET /v1/admin/gateway-config`
- `PUT /v1/admin/gateway-config/{id}`
- `GET/POST /v1/admin/platform-settings`
- `GET/PUT/DELETE /v1/admin/platform-settings/{id}`

**邮件：**
- `GET /v1/admin/email/status`
- `GET /v1/admin/email/config`
- `POST /v1/admin/email/test`

## 4. SSE 事件规范

### 4.1 事件 Envelope

所有事件共用：

| 字段 | 说明 |
|------|------|
| `event_id` | 全局唯一 |
| `run_id` | 归属 run |
| `seq` | run 内单调递增序号 |
| `ts` | 服务端时间戳 |
| `type` | 事件类型 |
| `data_json` | 事件负载 |

### 4.2 事件类型

**Run 生命周期：**

| 类型 | 说明 |
|------|------|
| `run.started` | 运行开始 |
| `run.completed` | 运行完成 |
| `run.failed` | 运行失败（含 `error_class`） |
| `run.cancelled` | 运行被取消 |
| `run.cancel_requested` | 收到取消信号 |

**Human-in-the-loop：**

| 类型 | 说明 |
|------|------|
| `run.input_requested` | 等待用户输入 |
| `run.input_provided` | 用户已提交输入 |

**消息流：**

| 类型 | 说明 |
|------|------|
| `message.delta` | 模型流式增量（`content_delta`、`role`） |

**工具调用：**

| 类型 | 说明 |
|------|------|
| `tool.call` | 工具调用发起 |
| `tool.result` | 工具执行结果 |
| `tool.denied` | 工具被策略/资源限制拒绝 |

**Agent Loop 内部：**

| 类型 | 说明 |
|------|------|
| `run.route.selected` | Provider 路由选定 |
| `run.segment.start` | 迭代/段开始 |
| `run.segment.end` | 迭代/段结束 |
| `run.llm.retry` | LLM 重试 |
| `run.provider_fallback` | Provider 回退 |

**Lua 执行器扩展：**

| 类型 | 说明 |
|------|------|
| `agent.parallel_dispatch` | 并行执行调度 |
| `agent.parallel_complete` | 并行执行完成 |

**调试事件（`ARKLOOP_LLM_DEBUG_EVENTS=1` 开启，仅限本地/测试）：**

| 类型 | 说明 |
|------|------|
| `llm.request` | 上游请求 payload（不含 secret） |
| `llm.response.chunk` | 上游流式原始 chunk |

### 4.3 关联约束

- `tool.call` / `tool.result` 通过 `tool_call_id` 关联
- 同一 run 内 `seq` 严格递增
- 事件先落库（`run_events` 表），再通过 PG `LISTEN/NOTIFY` 推送

## 5. 错误模型

统一错误响应：

```json
{
  "code": "auth.invalid_credentials",
  "message": "...",
  "details": {},
  "trace_id": "..."
}
```

HTTP Header 同时返回 `X-Trace-Id`。

错误分类：

| 前缀 | 说明 |
|------|------|
| `auth.*` | 鉴权/权限 |
| `validation.*` | schema 校验 |
| `policy.*` | 策略拦截 |
| `budget.*` | 预算/配额 |
| `provider.*` | 模型提供商错误 |
| `mcp.*` | MCP 协议错误（timeout/disconnected/rpc_error/protocol_error/tool_error） |
| `internal.*` | 内部错误 |

`trace_id` 由服务端生成；受信任上游（网关）可透传 `X-Trace-Id`（`ARKLOOP_TRUST_INCOMING_TRACE_ID=1`），普通客户端的不可信。

## 6. 中间件栈

请求处理顺序：

1. **TraceMiddleware** -- 生成/验证 trace_id，解析客户端 IP
2. **RecoverMiddleware** -- panic 恢复与错误日志
3. **Auth Middleware** -- Token 验证，角色检查
4. **Entitlement Middleware** -- 配额/功能权限检查
5. **Audit Logging** -- 写入 `audit_logs` 表

## 7. 配置（API 相关 env）

| 变量 | 说明 |
|------|------|
| `ARKLOOP_API_GO_ADDR` | 监听地址（默认 `127.0.0.1:19001`） |
| `ARKLOOP_DATABASE_URL` | PostgreSQL 连接 |
| `ARKLOOP_DATABASE_DIRECT_URL` | 直连（SSE LISTEN/NOTIFY，绕过 PgBouncer） |
| `ARKLOOP_REDIS_URL` | Redis（速率限制、运行并发控制） |
| `ARKLOOP_AUTH_JWT_SECRET` | JWT 签名密钥 |
| `ARKLOOP_ENCRYPTION_KEY` | AES-256-GCM 密钥（64 hex） |
| `ARKLOOP_S3_*` | S3 兼容对象存储 |
| `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` | 初始管理员 user_id（UUID） |
| `ARKLOOP_TRUST_INCOMING_TRACE_ID` | 信任上游 trace_id |
| `ARKLOOP_TRUST_X_FORWARDED_FOR` | 信任 X-Forwarded-For |
| `ARKLOOP_MAX_CONCURRENT_RUNS_PER_ACCOUNT` | 账户并发 run 上限（默认 10） |
| `ARKLOOP_SSE_HEARTBEAT_SECONDS` | SSE 心跳间隔（默认 15） |
| `ARKLOOP_SSE_BATCH_LIMIT` | SSE 批次上限（默认 500） |
| `ARKLOOP_RUN_TIMEOUT_MINUTES` | Run 超时（默认 5） |
| `ARKLOOP_RUN_EVENTS_RETENTION_MONTHS` | 事件分区保留月数（默认 3） |
| `ARKLOOP_APP_BASE_URL` | 前端地址 |
| `ARKLOOP_TURNSTILE_*` | Cloudflare Captcha |
| `ARKLOOP_EMAIL_FROM` | 发件地址 |

## 8. SSE 鉴权

采用 **Fetch 流式 + `Authorization: Bearer`**：
- SSE 与普通 API 使用同一鉴权机制
- Web/CLI 统一实现
- 前端通过 `after_seq` 游标断线重连
- `run_events` 不包含敏感明文（模型 key、system prompt 原文）
