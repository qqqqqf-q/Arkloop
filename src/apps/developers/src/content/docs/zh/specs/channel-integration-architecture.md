---
---

# 社交平台 Channel 接入架构

本文描述 Arkloop 接入 Telegram / Discord / 飞书等社交平台的完整架构设计。核心目标：将 Agent 从 Web-only 扩展为多平台可达，同时复用现有 Pipeline、Memory、Tool 体系。

## 1. 设计约束

1. **不新增微服务**。Webhook 接收在 API 服务，消息处理在 Worker，与现有 run 执行链路一致。
2. **不改 Pipeline 核心**。通过新增中间件和 post-hook 扩展，不修改现有中间件逻辑。
3. **平台接入是通用能力**。任何 Persona 都可绑定任何平台，不是专属 Agent 的行为。平台有优化过的 Persona 模板（控制输出风格），但这是可选的。
4. **Channel Config 是 org 级资源**。Bot Token 属于组织，不属于 Persona。
5. **Memory 天然复用**。`MemoryIdentity{OrgID, UserID, AgentID}` 不变，只需将平台用户映射到 Arkloop UserID。

## 2. 术语

| 术语 | 含义 |
|------|------|
| Channel | 一个社交平台的接入实例（如一个 Telegram Bot） |
| Channel Type | 平台类型：`telegram`、`discord`、`feishu` |
| Channel Binding | Channel 与 Persona 的绑定关系 |
| DM | 私聊（Direct Message），一对一对话 |
| Group | 群聊，多用户共享的聊天空间 |
| Connector | Channel 层的适配逻辑（格式转换、命令路由、消息池） |
| Shadow User | 未注册的平台用户在系统中的临时身份 |

## 3. 整体拓扑

```
Telegram/Discord/Feishu
         |
     Webhook POST
         |
         v
  Gateway (rate limit, trace)
         |
         v
  API Service
  ├── /v1/channels/{type}/webhook  (Connector: 验签, 命令路由, 消息入池)
  ├── 创建 Thread + Message + Run
  └── Enqueue job
         |
         v
  Worker (Pipeline)
  ├── 现有中间件链
  ├── mw_channel_context (新增: 注入平台上下文, 动态加载平台 ToolProvider)
  ├── handler_agent_loop (不变)
  └── post-hook: ChannelSender (新增: 格式转换 + 平台 API 回推)
         |
         v
  Telegram/Discord/Feishu API (发送响应)
```

## 4. 数据模型

### 4.1 `channels`

org 级 Channel 配置。一行 = 一个 Bot 实例。

```sql
CREATE TABLE channels (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    channel_type    TEXT        NOT NULL,  -- "telegram" | "discord" | "feishu"
    name            TEXT        NOT NULL,  -- 显示名称，如 "客服 Bot"
    credentials_id  UUID        REFERENCES secrets(id),  -- Bot Token（加密存储）
    webhook_secret  TEXT,       -- 平台 Webhook 验签密钥
    webhook_url     TEXT,       -- 系统生成的 Webhook 回调 URL
    is_active       BOOLEAN     NOT NULL DEFAULT FALSE,
    config_json     JSONB       NOT NULL DEFAULT '{}',  -- 平台特定配置
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, channel_type, credentials_id)
);
```

`config_json` 示例（Telegram）：
```json
{
  "bot_username": "arkloop_bot",
  "allowed_updates": ["message", "callback_query"],
  "group_privacy_mode": false
}
```

### 4.2 `channel_bindings`

Channel 与 Persona 的绑定关系。多对多，但同一 `(channel_id, chat_scope)` 同时只绑定一个 Persona。

```sql
CREATE TABLE channel_bindings (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id      UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    persona_id      UUID        NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    chat_scope      TEXT        NOT NULL DEFAULT 'all',  -- "all" | 特定 chat_id
    mode            TEXT        NOT NULL DEFAULT 'dm',    -- "dm" | "group" | "both"
    config_json     JSONB       NOT NULL DEFAULT '{}',
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel_id, chat_scope, mode)
);
```

`config_json` 字段承载 Persona 在该 Binding 下的平台行为配置：

```json
{
  "group_sliding_window": 50,
  "group_max_context_tokens": 4096,
  "auto_reply_dm": true,
  "trigger_on_reply": true
}
```

### 4.3 `channel_messages`

群聊消息池。独立于 `messages` 表，存储 Bot 可见的群聊消息。

```sql
CREATE TABLE channel_messages (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id          UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT        NOT NULL,  -- 平台群聊 ID
    platform_message_id TEXT        NOT NULL,  -- 平台消息 ID
    platform_user_id    TEXT        NOT NULL,  -- 发送者平台 ID
    platform_username   TEXT,
    content_text        TEXT,                   -- 文本内容
    content_json        JSONB,                  -- 富媒体内容（图片/文件/语音的元数据）
    reply_to_message_id TEXT,                   -- 引用的消息 ID
    message_type        TEXT        NOT NULL DEFAULT 'text',  -- "text" | "image" | "voice" | "file" | "sticker"
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_messages_chat_time
    ON channel_messages (channel_id, platform_chat_id, created_at DESC);
```

### 4.4 `channel_user_links`

平台用户与 Arkloop 用户的绑定关系。

```sql
CREATE TABLE channel_user_links (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              UUID        NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    channel_type        TEXT        NOT NULL,
    platform_user_id    TEXT        NOT NULL,
    user_id             UUID        REFERENCES users(id) ON DELETE SET NULL,  -- NULL = 未绑定
    platform_username   TEXT,
    verified            BOOLEAN     NOT NULL DEFAULT FALSE,
    bind_token          TEXT,       -- 一次性绑定 token
    bind_token_expires  TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, channel_type, platform_user_id)
);
```

## 5. Webhook 接收（API 层）

### 5.1 路由

```
POST /v1/channels/telegram/{channel_id}/webhook
POST /v1/channels/discord/{channel_id}/webhook
POST /v1/channels/feishu/{channel_id}/webhook
```

`channel_id` 嵌入 URL 中，API 根据它查找 `channels` 行并验证签名。不经过 JWT 认证（平台 Webhook 没有 Bearer Token），但必须验签。

### 5.2 处理流程

```
1. 验签（Telegram: secret_token header; Discord: Ed25519; 飞书: SHA256 HMAC）
2. 解析平台消息格式 -> 统一内部表示 ChannelIncomingMessage
3. 命令路由：
   a. /start, /help, /bind -> Connector 直接处理，不经过 Agent
   b. 其他 -> 继续
4. 写入 channel_messages（群聊消息池，所有 Bot 可见消息都写入）
5. 判断是否触发 Agent：
   a. DM: 始终触发
   b. Group: @-mention 或回复 Bot 消息时触发
6. 触发时：
   a. 解析 channel_binding -> 确定 Persona
   b. 解析 channel_user_link -> 确定 UserID（无绑定则 NULL）
   c. DM: 查找或创建持久 Thread（per user + per persona + per channel）
   d. Group: 创建新 Thread，将滑动窗口作为 context 注入
   e. 创建 Message + Run
   f. Enqueue job（payload 携带 channel_delivery_meta）
```

### 5.3 ChannelIncomingMessage（统一内部表示）

```go
type ChannelIncomingMessage struct {
    ChannelID       uuid.UUID
    ChannelType     string            // "telegram" | "discord" | "feishu"
    PlatformChatID  string            // 群/私聊 ID
    PlatformMsgID   string            // 消息 ID
    PlatformUserID  string            // 发送者 ID
    PlatformUsername string
    ChatType        string            // "private" | "group" | "supergroup" | "channel"
    Text            string
    MediaAttachments []MediaAttachment // 图片/文件/语音
    ReplyToMsgID    *string           // 引用消息 ID
    MentionsBot     bool              // 是否 @Bot
    RawPayload      json.RawMessage   // 原始平台数据
}
```

## 6. Worker 扩展

### 6.1 Job Payload 扩展

`jobs.payload_json` 新增 `channel_delivery` 字段：

```json
{
  "run_id": "...",
  "thread_id": "...",
  "channel_delivery": {
    "channel_id": "uuid",
    "channel_type": "telegram",
    "platform_chat_id": "123456",
    "reply_to_message_id": "789",
    "group_context_messages": [...]
  }
}
```

当 `channel_delivery` 为 `null` 时，行为与现有 Web 流程完全一致。

### 6.2 mw_channel_context（新增中间件）

插入位置：`mw_persona_resolution` 之后，`mw_tool_build` 之前。

职责：
1. 从 job payload 读取 `channel_delivery`，无则跳过
2. 将 `ChannelType` 写入 `RunContext.ChannelContext`
3. 群聊模式时，将滑动窗口消息注入 `rc.Messages`（作为 context 前缀）
4. 注册对应平台的 ToolProvider（动态注入平台工具）

```go
type ChannelContext struct {
    ChannelID          uuid.UUID
    ChannelType        string
    PlatformChatID     string
    ReplyToMessageID   *string
    IsGroupChat        bool
    GroupContextWindow []ChannelPoolMessage
}
```

### 6.3 平台 ToolProvider

每个平台注册一个 ToolProvider，当 `RunContext.ChannelContext` 存在时自动激活。

Telegram 工具示例：

| 工具名 | 说明 |
|--------|------|
| `telegram_reply` | 回复引用特定消息 |
| `telegram_react` | 给消息添加 emoji reaction |
| `telegram_pin` | 置顶消息 |
| `telegram_send_photo` | 发送图片 |
| `telegram_send_document` | 发送文件 |

Discord/飞书同理，按平台能力定义工具集。

工具执行时通过 `ChannelContext` 获取 chat_id、bot token 等信息，直接调用平台 API。

### 6.4 Post-hook: ChannelSender

在 `handler_agent_loop` 完成后执行。位于 Pipeline 的 defer 逻辑中（类似现有 webhook enqueue）。

职责：
1. 检查 `rc.ChannelContext` 是否存在，无则跳过
2. 提取 `rc.FinalAssistantOutput`
3. 格式转换：Markdown -> 平台格式（Telegram MarkdownV2 / Discord Markdown / 飞书富文本 JSON）
4. 长消息自动分段（Telegram: 4096 字符，Discord: 2000 字符）
5. 调用平台 API 发送
6. 群聊时设置 `reply_to_message_id`（回复触发消息）

```go
type ChannelSender interface {
    Send(ctx context.Context, channelCtx ChannelContext, output string) error
}
```

每个平台实现一个 Sender：`TelegramSender`、`DiscordSender`、`FeishuSender`。

## 7. 用户绑定与 Memory

### 7.1 验证式绑定流程

```
用户（Web）                      Bot（Telegram）
    |                                |
    |-- 点击「绑定 Telegram」-------->|
    |                                |
    |<-- 返回一次性 token ------------|
    |    (存入 channel_user_links    |
    |     bind_token, 5min 过期)     |
    |                                |
    |-- 向 Bot 发送 /bind <token> -->|
    |                                |
    |    Bot 验证 token              |
    |    匹配 -> 写入 user_id        |
    |    verified = true             |
    |                                |
    |<-- 绑定成功 -------------------|
```

或通过 Telegram Deep Link：`t.me/{bot_username}?start=bind_{token}`

### 7.2 未绑定用户处理

1. Bot 收到消息，查 `channel_user_links` 找不到绑定 -> 创建记录（`user_id = NULL`）
2. Run 执行时 `rc.UserID = nil`，`MemoryMiddleware` 跳过写入（已有此逻辑）
3. Agent 正常响应，但无长期 Memory
4. 用户后续注册 + 绑定后，`user_id` 填充，Memory 开始积累

### 7.3 DM Memory 一致性

私聊 Thread 按 `(channel_id, platform_user_id, persona_id)` 唯一确定。绑定用户后，该 Thread 的后续 run 携带 `UserID`，Memory 与 Web 端共享同一个 `MemoryIdentity{OrgID, UserID, AgentID}`。

## 8. 群聊交互模型

### 8.1 消息池写入

所有 Bot 可见消息写入 `channel_messages`，不区分是否触发 Agent。写入是异步的（API 层在 Webhook handler 中同步写入，成本低）。

### 8.2 @-mention 触发

触发条件（满足任一）：
- 消息中 @Bot
- 回复 Bot 的消息

触发时：
1. 从 `channel_messages` 读取最近 N 条（N 由 `channel_bindings.config_json.group_sliding_window` 配置）
2. 按 token 预算动态裁剪（`config_json.group_max_context_tokens`）
3. 创建新 Thread，滑动窗口作为 system prompt 的 context section 注入
4. 用户消息（@内容）作为该 Thread 的第一条 user message

### 8.3 输出约束

群聊场景下 Agent 的输出特征应由 Persona 的 System Prompt 控制：
- 简洁回复（不是长篇大论）
- 隐藏思维过程、工具调用细节
- 适合群聊阅读的格式

这不是 Connector 层强制的，而是 Persona 设计者通过 System Prompt 约束的。如果用户把 Normal Persona 接入群聊，输出会很长 -- 这是预期行为，不是 bug。

### 8.4 事件过滤

群聊 run 的 `run_events` 仍然正常写入（审计需要）。但 ChannelSender 只关心 `FinalAssistantOutput`，不回推中间事件（thinking、tool_call 等）。Web Console 仍可回放完整事件流。

## 9. Console 管理界面

### 9.1 导航位置

在 ConsoleLayout 的 `integration` 分组下新增：

```
Integration
├── API Keys      (已有)
├── Webhooks      (已有, placeholder)
└── Channels      (新增)
```

### 9.2 Channels 页面

列表视图：

| 列 | 说明 |
|----|------|
| 名称 | Channel 显示名 |
| 平台 | Telegram / Discord / 飞书（带图标） |
| 状态 | Active / Inactive |
| 绑定 | 已绑定的 Persona 列表 |
| Webhook URL | 复制按钮 |

详情/编辑视图：

- 基本信息：名称、平台类型
- 凭证：Bot Token（密文显示，关联 secrets 表）
- Webhook：系统生成的 URL，一键复制，可重新生成
- 绑定管理：关联的 Persona 列表，可增删，配置 mode（dm/group/both）和 chat_scope
- 平台配置：JSON 编辑器（group_privacy_mode 等）

### 9.3 Persona 详情页扩展

在 Persona 编辑页新增 "Channels" tab：
- 显示该 Persona 已绑定的 Channel 列表
- 可从此处快速创建/编辑绑定
- 双向操作：Channel 页可选 Persona，Persona 页可选 Channel

## 10. API 端点

### 10.1 Channel CRUD

```
POST   /v1/channels                    -- 创建 Channel
GET    /v1/channels                    -- 列出 org 的 Channels
GET    /v1/channels/{id}               -- 获取详情
PATCH  /v1/channels/{id}               -- 更新
DELETE /v1/channels/{id}               -- 删除

POST   /v1/channels/{id}/bindings      -- 创建 Binding
GET    /v1/channels/{id}/bindings      -- 列出 Bindings
PATCH  /v1/channels/{id}/bindings/{bid} -- 更新 Binding
DELETE /v1/channels/{id}/bindings/{bid} -- 删除 Binding
```

### 10.2 用户绑定

```
POST   /v1/me/channel-links            -- 发起绑定（生成 token）
GET    /v1/me/channel-links            -- 查看已绑定的平台账号
DELETE /v1/me/channel-links/{id}       -- 解除绑定
```

### 10.3 Webhook 接收

```
POST   /v1/channels/telegram/{channel_id}/webhook
POST   /v1/channels/discord/{channel_id}/webhook
POST   /v1/channels/feishu/{channel_id}/webhook
```

这些端点不经过 JWT 认证，通过平台签名验证。Gateway 仍做 rate limit 和 trace。

## 11. 格式转换

### 11.1 Markdown -> 平台格式

| 平台 | 目标格式 | 关键差异 |
|------|----------|----------|
| Telegram | MarkdownV2 | 特殊字符需转义（`_*[]()~>#+-=\|{}.!`） |
| Discord | Discord Markdown | 基本兼容，代码块用 ` ``` ` |
| 飞书 | 富文本 JSON | 完全不同的结构化格式 |

Connector 层实现 `Formatter` 接口：

```go
type Formatter interface {
    Format(markdown string) (string, error)
    MaxMessageLength() int
}
```

### 11.2 长消息分段

当 `Format` 输出超过 `MaxMessageLength()` 时，按以下优先级切分：
1. 段落边界（`\n\n`）
2. 句子边界（`。` `.` `!` `?`）
3. 硬切（`MaxMessageLength - 10` 字符处）

分段后依次发送，每段间隔 50ms（避免平台 rate limit）。

## 12. 平台特定适配

### 12.1 Telegram

- Webhook 注册：`POST https://api.telegram.org/bot{token}/setWebhook`
- 验签：`X-Telegram-Bot-Api-Secret-Token` header
- 消息发送：`POST /sendMessage`，支持 `reply_to_message_id` 和 `parse_mode: MarkdownV2`
- 媒体：`getFile` 下载 -> 上传到 MinIO -> 作为 attachment 传入 Agent
- Bot Commands：通过 `setMyCommands` 注册 `/start`、`/help`、`/bind`
- Group Privacy：由管理员在 BotFather 配置，决定 Bot 可见消息范围

### 12.2 Discord

- Webhook 注册：Discord Developer Portal 配置 Interactions Endpoint URL
- 验签：Ed25519 签名验证
- 消息发送：`POST /channels/{channel_id}/messages`
- 媒体：通过 CDN URL 直接访问
- Slash Commands：注册 Application Commands

### 12.3 飞书

- Webhook 注册：飞书开放平台配置事件订阅
- 验签：SHA256 HMAC
- 消息发送：`POST /open-apis/im/v1/messages`
- 格式：富文本 JSON（`post` 类型）
- 消息卡片：可选，用于复杂交互

## 13. 安全

### 13.1 Webhook 验签

每个平台有独立的验签逻辑，必须在处理消息前完成。验签失败返回 `401`。

### 13.2 Bot Token 存储

复用现有 `secrets` 表的加密存储（`ARKLOOP_ENCRYPTION_KEY`），与 LLM Provider 凭证同等安全级别。

### 13.3 用户绑定安全

- 绑定 token 有效期 5 分钟
- 一次性使用，验证后立即作废
- 不允许自填 platform_user_id（必须通过 Bot 交互验证）

### 13.4 Rate Limit

- Gateway 层：per-IP rate limit（已有）
- API 层：per-channel rate limit（新增，防止单个群聊刷爆 Agent）
- 建议默认值：每 channel 每分钟 30 次 Agent 触发

## 14. 实现分阶段

### Phase 1: 基础设施

- 数据模型（4 张表 + migrations）
- Channel CRUD API + Console Channels 页面
- 用户绑定 API + 验证流程

### Phase 2: Telegram 私聊

- Telegram Webhook 接收 + 验签
- Connector 层（消息解析、命令路由）
- DM Thread 映射
- Worker mw_channel_context
- ChannelSender（Telegram）
- 格式转换（Markdown -> MarkdownV2）
- 长消息分段

### Phase 3: Telegram 群聊

- channel_messages 消息池写入
- @-mention 触发逻辑
- 滑动窗口 + token 预算裁剪
- 群聊 Thread 创建
- 媒体文件入池（图片/语音/文件下载 -> MinIO）

### Phase 4: 平台工具

- Telegram ToolProvider（reply、react、pin 等）
- Persona 详情页 Channels tab
- Channel Binding 双向管理 UI

### Phase 5: 多平台扩展

- Discord Connector + Sender + ToolProvider
- 飞书 Connector + Sender + ToolProvider
- 平台工具统一抽象（跨平台共性工具）

## 15. 配置（新增 env）

| 变量 | 说明 |
|------|------|
| `ARKLOOP_CHANNEL_RATE_LIMIT_PER_MIN` | 每 channel 每分钟最大 Agent 触发数（默认 30） |
| `ARKLOOP_CHANNEL_BIND_TOKEN_TTL_SEC` | 绑定 token 有效期（默认 300） |
| `ARKLOOP_CHANNEL_GROUP_DEFAULT_WINDOW` | 群聊默认滑动窗口大小（默认 50） |
| `ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS` | 群聊最大上下文 token 数（默认 4096） |
| `ARKLOOP_CHANNEL_MESSAGE_SEGMENT_DELAY_MS` | 分段消息发送间隔（默认 50） |

## 16. 开放问题

以下问题在实现过程中需要进一步确认：

1. **Credit 消耗**：org 统一承担。自部署用户通常关闭积分系统，无影响。SaaS 场景需要在 Console 中展示 Channel 产生的用量统计。
2. **消息池清理策略**：`channel_messages` 会持续增长。需要 TTL 清理（如保留 30 天）或按量上限清理。
3. **Webhook 重试**：平台 Webhook 超时后会重试。API 层需要幂等处理（基于 `platform_message_id` 去重）。
4. **Bot 离线期间的消息**：Bot 不在线时平台会暂存消息。恢复后 Webhook 会集中推送，需要防止雪崩。
5. **多 Persona 路由**：当同一 Channel 绑定多个 Persona（不同 chat_scope）时，路由逻辑需要明确优先级。
