---
---

# 社交平台 Channel 接入架构

本文描述 Arkloop 接入 Telegram / Discord / 飞书等社交平台的完整架构设计。核心目标：将 Agent 从 Web-only 扩展为多平台可达，同时复用现有 Pipeline、Memory、Tool 体系。

## 1. 设计约束

1. **不新增微服务**。Webhook 接收在 API 服务，消息处理在 Worker，与现有 run 执行链路一致。
2. **不改 Pipeline 核心**。通过新增中间件和 post-hook 扩展，不修改现有中间件逻辑。
3. **平台接入是通用能力**。任何 Persona 都可通过 Channel 对外服务。
4. **Channel Config 是 Account 级资源**。Bot Token 属于 Account（类似 BYOK），一个 Account 每个平台最多一个 Bot Token，默认关联到当前活跃 Persona。
5. **Memory 按 UserID 锚定**。`MemoryIdentity` 为 `{AccountID, UserID, AgentID}`。绑定后 `channel_identities.user_id` 与 DM Thread 指向正式用户，**之后**的新 Run 与 Web 共用同一 UserID；shadow 阶段已写入的长期记忆 **不会** 随绑定自动搬迁（批量迁移见 7.3 现状）。
6. **Credit 由用户承担**。Channel 产生的消耗扣用户 credit。self-host / local-only 场景下扣到本地默认用户。

### 1.1 能力阶段（Telegram 私信）

| 档位 | 内容 |
|------|------|
| 基础 | Webhook 或 Desktop `getUpdates`、DM thread、`/new`、`run.started` 模型、`channel_message_ledger` 入站 |
| 投递 | `sendMessage`、`reply_to_message_id`、分段、`channel_message_deliveries` + 出站 ledger |
| 交互 | `sendChatAction`（typing，可 `config_json.telegram_typing_indicator` 关闭）、`setMessageReaction`（`telegram_reaction_emoji`，空则关）；`editMessageText` 已在共享 `telegrambot.Client` 暴露，Agent loop 流式编辑未接 |
| 登记读 | Worker `LookupByPlatformMessage(channel_id, platform_conversation_id, platform_message_id)` |

## 2. 术语

| 术语 | 含义 |
|------|------|
| Channel | 一个社交平台的接入实例（如一个 Telegram Bot） |
| Channel Type | 平台类型：`telegram`、`discord`、`feishu` |
| Channel Identity | 平台用户在系统中的跨平台统一身份主体 |
| Shadow User | 未注册的平台用户在系统中自动创建的轻量用户记录（不可登录，仅作 Memory 锚点） |
| DM | 私聊（Direct Message），一对一对话 |
| Group | 群聊，多用户共享的聊天空间 |
| Passive Message | Bot 可见但未触发 Run 的群聊消息（写入 Thread 但不创建 Run） |
| Active Message | @Bot 的消息，触发 Run |
| Connector | Channel 层的适配逻辑（格式转换、命令路由） |
| Bind Code | 一次性验证码，用于将平台身份关联到 Arkloop 账号 |

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
  ├── /v1/channels/{type}/webhook  (Connector: 验签, 命令路由)
  ├── 被动消息 -> 写入群聊 Thread (不创建 Run)
  ├── 主动消息 (@Bot) -> 创建 Message + Run
  └── Enqueue job
         |
         v
  Worker (Pipeline)
  ├── 现有中间件链
  ├── mw_channel_context（解析 channel_delivery，注入 ChannelContext / sender UserID）
  ├── handler_agent_loop
  └── mw_channel_delivery（投递、typing、可选 reaction、ledger）
         |
         v
  Telegram/Discord/Feishu API (发送响应)
```

## 4. 数据模型

### 4.1 `channels`

Account 级 Channel 配置。一行 = 一个 Bot 实例。每个 Account 每个平台最多一个。

```sql
CREATE TABLE channels (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel_type    TEXT        NOT NULL,  -- "telegram" | "discord" | "feishu"
    persona_id      UUID        REFERENCES personas(id) ON DELETE SET NULL,  -- 关联的 Persona
    credentials_id  UUID        REFERENCES secrets(id),  -- Bot Token（加密存储）
    webhook_secret  TEXT,       -- 平台 Webhook 验签密钥
    webhook_url     TEXT,       -- 系统生成的 Webhook 回调 URL
    is_active       BOOLEAN     NOT NULL DEFAULT FALSE,
    config_json     JSONB       NOT NULL DEFAULT '{}',  -- 平台特定配置
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, channel_type)
);
```

`config_json` 示例（Telegram）：
```json
{
  "bot_username": "arkloop_bot",
  "allowed_updates": ["message", "callback_query"],
  "group_privacy_mode": false,
  "default_model": "openai^gpt-4.1-mini",
  "telegram_typing_indicator": true,
  "telegram_reaction_emoji": ""
}
```

- `telegram_typing_indicator`：缺省按 `true`；`false` 时不发 typing。
- `telegram_reaction_emoji`：非空时在成功投递后对**用户入站消息**调用 `setMessageReaction`；空字符串表示关闭。

### 4.2 `channel_identities`

跨平台统一身份主体。每个平台用户在系统中对应一条记录。

```sql
CREATE TABLE channel_identities (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type        TEXT        NOT NULL,
    platform_subject_id TEXT        NOT NULL,  -- 平台用户唯一标识
    user_id             UUID        REFERENCES users(id) ON DELETE SET NULL,  -- 关联的系统用户（可为 shadow user）
    display_name        TEXT,
    avatar_url          TEXT,
    metadata            JSONB       NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel_type, platform_subject_id)
);
```

### 4.3 `channel_identity_bind_codes`

一次性绑定验证码，用于将平台身份关联到正式 Arkloop 账号。

```sql
CREATE TABLE channel_identity_bind_codes (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    token                       TEXT        NOT NULL UNIQUE,  -- 8 位大写字母数字
    issued_by_user_id           UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type                TEXT,       -- 限定平台（可选）
    used_at                     TIMESTAMPTZ,
    used_by_channel_identity_id UUID        REFERENCES channel_identities(id),
    expires_at                  TIMESTAMPTZ NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 4.4 Shadow User

未注册平台用户在系统中的轻量用户记录。复用现有 `users` 表，通过 `source` 字段区分：

```sql
ALTER TABLE users ADD COLUMN source TEXT NOT NULL DEFAULT 'web';
-- source 取值: 'web' (正常注册) | 'channel_shadow' (Channel 自动创建)
```

Shadow User 特征：
- `source = 'channel_shadow'`
- 不可登录（无密码、无 OAuth 绑定）
- 自动创建 personal Account
- Memory 长期保留，不清理
- 绑定正式账号后：**身份与 Thread owner** 切到正式用户；**向量 / 快照记忆的批量迁移** 当前未实现（见 7.3）

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
4. 解析发送者 -> upsert channel_identities (自动创建或更新)
5. 未关联用户 -> 自动创建 shadow user + personal account
6. 判断消息类型：
   a. DM:
      - 查找或创建持久 Thread (per channel + per channel_identity + per persona)
      - 写入 Message, 创建 Run, Enqueue job
   b. Group (未 @Bot):
      - 被动消息: 写入群聊 Thread (带发送者 header), 不创建 Run
   c. Group (@Bot 或回复 Bot 消息):
      - 主动消息: 写入群聊 Thread, 创建 Run, Enqueue job
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

### 5.4 Thread 映射规则

| 场景 | Thread 唯一键 | Thread 生命周期 |
|------|---------------|-----------------|
| DM | `(channel_id, channel_identity_id, persona_id)` | 持久，用户可在 Web 端看到 |
| Group | `(channel_id, platform_chat_id, persona_id)` | 持久，所有群成员共享 |

### 5.5 消息写入格式

所有写入 Thread 的消息（被动/主动）携带结构化 header，供 LLM 识别发送者：

```yaml
---
channel-identity-id: "uuid"
display-name: "Alice"
channel: "telegram"
conversation-type: "group"
time: "2026-03-16T10:00:00Z"
---
用户的实际消息内容
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
    "sender_channel_identity_id": "uuid"
  }
}
```

当 `channel_delivery` 为 `null` 时，行为与现有 Web 流程完全一致。

### 6.2 mw_channel_context

插入位置以代码 `runengine` 为准（在 Persona 解析之后，便于带上 `sender_channel_identity_id` 解析出的 `UserID`）。

职责：
1. 从 job payload 读取 `channel_delivery`，无则跳过
2. 将 `ChannelType` 写入 `RunContext.ChannelContext`
3. 根据 `sender_channel_identity_id` 解析发送者的 UserID，用于 MemoryMiddleware
4. 注册对应平台的 ToolProvider（动态注入平台工具）

结构体字段以 `worker/internal/pipeline/mw_channel_context.go` 为准（含 `Conversation`、`InboundMessage`、`TriggerMessage` 等）。

### 6.3 Memory 加载逻辑

群聊 Run 的 Memory 按发送者隔离：

```
群聊 Run:
  sender = channel_identity -> user_id (real or shadow)
  MemoryIdentity = {AccountID, sender.UserID, PersonaID}
  -> 加载该用户的长期 Memory
  -> Thread 历史提供群聊语境 (所有人的消息)
  -> 两者互补: Memory = 个人记忆, Thread = 公共语境

DM Run:
  与现有逻辑一致
  MemoryIdentity = {AccountID, UserID, PersonaID}
```

### 6.4 平台 ToolProvider

每个平台注册一个 ToolProvider，当 `RunContext.ChannelContext` 存在时自动激活。

Telegram 工具示例：

| 工具名 | 说明 |
|--------|------|
| `telegram_reply` | 回复引用特定消息（投递链已 `reply_to`；工具层可再暴露） |
| `telegram_react` | 表情反应（投递成功后可配置 `telegram_reaction_emoji`；独立工具可后续接 `setMessageReaction`） |
| `telegram_pin` | 置顶消息 |
| `telegram_send_photo` | 发送图片 |
| `telegram_send_document` | 发送文件 |

Discord/飞书同理，按平台能力定义工具集。

工具执行时通过 `ChannelContext` 获取 chat_id、bot token 等信息，直接调用平台 API。

### 6.5 Post-hook: ChannelSender（`mw_channel_delivery`）

在 `handler_agent_loop` **之后**执行（中间件包裹 terminal，出站侧后处理）。

职责：
1. 检查 `rc.ChannelContext` 是否存在，无则跳过
2. Agent 循环期间可选 **Telegram typing**（ goroutine 周期 `sendChatAction`）
3. 提取 `rc.FinalAssistantOutput`
4. MarkdownV2 转义、分段（Telegram 4096）
5. `sendMessage`，带 `reply_to_message_id` / `message_thread_id`
6. 写入 `channel_message_deliveries` 与 `channel_message_ledger`（outbound）
7. 可选对用户入站消息 `setMessageReaction`

```go
type ChannelSender interface {
    Send(ctx context.Context, channelCtx ChannelContext, output string) error
}
```

每个平台实现一个 Sender：`TelegramSender`、`DiscordSender`、`FeishuSender`。

## 7. 用户身份与绑定

### 7.1 Channel Identity 自动创建

首次看到平台用户时自动创建：

```
1. Webhook 收到消息，提取 platform_user_id
2. Upsert channel_identities (channel_type, platform_subject_id)
3. 如果 user_id = NULL:
   a. 创建 shadow user (source = 'channel_shadow')
   b. 自动创建 personal account
   c. 将 channel_identity.user_id 指向 shadow user
4. 后续消息复用已有的 channel_identity
```

### 7.2 Bind Code 绑定流程

用户将平台身份关联到正式 Arkloop 账号：

```
用户（Web）                      Bot（Telegram）
    |                                |
    |-- 点击「绑定 Telegram」-------->|
    |                                |
    |<-- 返回 bind code (8 位) ------|
    |    (存入 bind_codes 表,        |
    |     24h 过期)                  |
    |                                |
    |-- 向 Bot 发送 /bind <code> --->|
    |                                |
    |    Bot 验证 code               |
    |    channel_identity.user_id    |
    |      从 shadow_user 更新为     |
    |      real_user                 |
    |                                |
    |    （OpenViking 记忆批量迁移     |
    |     当前未实现，见 7.3）         |
    |                                |
    |<-- 绑定成功 -------------------|
```

或通过 Telegram Deep Link：`t.me/{bot_username}?start=bind_{code}`

### 7.3 Shadow User Memory 迁移（规划 vs 现状）

**规划**（理想行为）：将 shadow `UserID` 下的向量 / 快照条目改写为正式用户，并合并 shadow 身份。

**现状（代码）**：绑定仅更新 `channel_identities.user_id`、相关 Thread `owner`、消费 bind code。不会扫描或改写 OpenViking / `user_memory_snapshots` 中旧 `user_id` 的数据。若需要与 Web 历史合一，须另做迁移 Job 或 alias 设计。

### 7.4 DM Memory 一致性

私聊 Thread 按 `(channel_id, channel_identity_id, persona_id)` 映射。绑定后新 Run 的 `UserID` 为正式用户，**之后**积累的记忆与 Web 同 identity；**绑定前** shadow 侧已写入的内容仍挂在旧 `user_id` 下，除非执行 7.3 的迁移。

## 8. 群聊交互模型

### 8.1 消息写入

群聊 Thread 存储 Bot 可见的所有消息：

| 消息类型 | 触发条件 | 写入 Thread | 创建 Run |
|----------|----------|-------------|----------|
| 被动消息 | 群聊中非 @Bot 的消息 | 是（带发送者 header） | 否 |
| 主动消息 | @Bot 或回复 Bot 消息 | 是 | 是 |

所有消息携带结构化 header 标识发送者，LLM 通过 header 区分群聊中的不同用户。

### 8.2 @-mention 触发

触发条件（满足任一）：
- 消息中 @Bot
- 回复 Bot 的消息

触发时：
1. 写入消息到群聊 Thread（同被动消息）
2. 解析发送者的 channel_identity -> user_id（real 或 shadow）
3. 创建 Run（携带 sender 的 UserID 用于 Memory）
4. Worker 加载 Thread 历史（按 token 预算裁剪，与现有 context trimming 一致）
5. Worker 加载 sender 的个人 Memory
6. Agent 响应

### 8.3 群聊 per-user Memory

群聊中每个 @Bot 的人都有独立的 Memory 积累：

1. 已绑定用户：Run 携带其正式 UserID，MemoryMiddleware 正常读写
2. Shadow 用户：Run 携带 shadow UserID，Memory 同样正常积累
3. 绑定正式账号后：身份切换，**新**记忆写入正式用户；历史 shadow 记忆不自动合并（见 7.3）

群聊 Run 的 MemoryIdentity 是 `{AccountID, sender.UserID, PersonaID}`，长期记忆按人隔离，群聊语境由 Thread 历史提供。

### 8.4 输出约束

群聊场景下 Agent 的输出特征应由 Persona 的 System Prompt 控制：
- 简洁回复（不是长篇大论）
- 隐藏思维过程、工具调用细节
- 适合群聊阅读的格式

这是 Persona 设计者通过 System Prompt 约束的，不是 Connector 层强制的。

### 8.5 事件过滤

群聊 Run 的 `run_events` 正常写入（审计需要）。ChannelSender 只关心 `FinalAssistantOutput`，不回推中间事件（thinking、tool_call 等）。Web 端仍可回放完整事件流。

## 9. Context Compaction

### 9.1 问题

Thread 历史会持续增长。群聊场景尤为明显（所有被动消息都写入 Thread），但普通 DM Thread 同样面临此问题。当 Thread 消息数超过 token 预算时，旧消息被裁剪丢弃，AI 丧失早期上下文。

### 9.2 方案

Context Compaction 是通用能力，适用于所有 Thread 类型：

```
Thread 消息历史:
  [msg_1] [msg_2] ... [msg_N] [msg_N+1] ... [msg_current]
  |___________________________|
        超出 token 预算的旧消息
                |
                v
        LLM 摘要压缩 -> compact_summary
                |
                v
  [compact_summary] [msg_N+1] ... [msg_current]
```

实现策略：
1. **触发条件**：Thread 消息总 token 数超过阈值（如 2x token budget）时触发
2. **压缩范围**：保留最近 N 条消息（在 token budget 内），将更早的消息用 LLM 压缩为摘要
3. **摘要存储**：作为特殊类型的 message 存入 Thread（`role = 'system'`, `type = 'compact_summary'`）
4. **增量压缩**：下次触发时，将旧摘要 + 新的溢出消息一起再次压缩
5. **群聊优化**：群聊摘要应保留每位参与者的关键发言和身份信息

### 9.3 与 Memory 的关系

Compaction 和 Memory 是互补的两层上下文管理：

| 层 | 范围 | 内容 | 持久性 |
|----|------|------|--------|
| Thread History | 近期 | 完整的消息流（在 token 预算内） | 随 Thread 生命周期 |
| Compact Summary | 中期 | 被压缩的早期对话摘要 | 随 Thread 生命周期 |
| Memory | 长期 | LLM 提取的结构化知识/偏好 | 永久，跨 Thread |

## 10. Web 端 UI

### 10.1 SettingsModal 新增 Tab

在 Web 端 SettingsModal 中新增 "Channels" / "Connectors" tab：

配置项：

| 字段 | 说明 |
|------|------|
| 平台选择 | Telegram / Discord / 飞书 |
| Bot Token | 密文输入，存入 secrets 表 |
| 状态 | Active / Inactive 开关 |
| Webhook URL | 系统生成，一键复制 |
| 关联 Persona | 下拉选择（默认当前活跃 Persona） |

### 10.2 绑定管理

在 SettingsModal 的 Channels tab 或独立区域：
- 显示已绑定的平台账号列表
- 生成 bind code 按钮
- 解绑按钮

### 10.3 群聊 Thread 可见性

群聊 Thread 在 Web 端的 Thread 列表中可见（标记来源为 Channel），用户可查看完整消息历史和 Run 事件流。

## 11. API 端点

### 11.1 Channel CRUD

```
POST   /v1/channels                    -- 创建 Channel
GET    /v1/channels                    -- 列出 Account 的 Channels
GET    /v1/channels/{id}               -- 获取详情
PATCH  /v1/channels/{id}               -- 更新
DELETE /v1/channels/{id}               -- 删除
```

### 11.2 用户绑定

```
POST   /v1/me/channel-binds            -- 生成 bind code
GET    /v1/me/channel-identities       -- 查看已绑定的平台身份
DELETE /v1/me/channel-identities/{id}  -- 解除绑定
```

### 11.3 Webhook 接收

```
POST   /v1/channels/telegram/{channel_id}/webhook
POST   /v1/channels/discord/{channel_id}/webhook
POST   /v1/channels/feishu/{channel_id}/webhook
```

这些端点不经过 JWT 认证，通过平台签名验证。Gateway 仍做 rate limit 和 trace。

## 12. 格式转换

### 12.1 Markdown -> 平台格式

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

### 12.2 长消息分段

当 `Format` 输出超过 `MaxMessageLength()` 时，按以下优先级切分：
1. 段落边界（`\n\n`）
2. 句子边界（`。` `.` `!` `?`）
3. 硬切（`MaxMessageLength - 10` 字符处）

分段后依次发送，每段间隔 50ms（避免平台 rate limit）。

## 13. 平台特定适配

### 13.1 Telegram

- Webhook 注册：`POST https://api.telegram.org/bot{token}/setWebhook`
- 验签：`X-Telegram-Bot-Api-Secret-Token` header
- 消息发送：`POST /sendMessage`，支持 `reply_to_message_id` 和 `parse_mode: MarkdownV2`
- 媒体：`getFile` 下载 -> 上传到 MinIO -> 作为 attachment 传入 Agent
- Bot Commands：通过 `setMyCommands` 注册 `/start`、`/help`、`/bind`
- Group Privacy：需关闭 privacy mode 才能收到所有群聊消息（BotFather 配置）

### 13.2 Discord

- Webhook 注册：Discord Developer Portal 配置 Interactions Endpoint URL
- 验签：Ed25519 签名验证
- 消息发送：`POST /channels/{channel_id}/messages`
- 媒体：通过 CDN URL 直接访问
- Slash Commands：注册 Application Commands

### 13.3 飞书

- Webhook 注册：飞书开放平台配置事件订阅
- 验签：SHA256 HMAC
- 消息发送：`POST /open-apis/im/v1/messages`
- 格式：富文本 JSON（`post` 类型）
- 消息卡片：可选，用于复杂交互

## 14. 安全

### 14.1 Webhook 验签

每个平台有独立的验签逻辑，必须在处理消息前完成。验签失败返回 `401`。

### 14.2 Bot Token 存储

复用现有 `secrets` 表的加密存储（`ARKLOOP_ENCRYPTION_KEY`），与 LLM Provider 凭证同等安全级别。

### 14.3 Bind Code 安全

- 有效期 24 小时
- 一次性使用，验证后立即作废
- 8 位大写字母数字，collision 自动重试
- 事务级原子操作（lock + consume）

### 14.4 Rate Limit

- Gateway 层：per-IP rate limit（已有）
- API 层：per-channel rate limit（新增，防止单个群聊刷爆 Agent）
- 建议默认值：每 channel 每分钟 30 次 Agent 触发

## 15. 实现分阶段

### Phase 1: 数据模型与 API 基础

#### 1a: 数据模型

- Migration: `channels` 表（account_id, channel_type, persona_id, credentials_id, webhook_secret, webhook_url, is_active, config_json）
- Migration: `channel_identities` 表（channel_type, platform_subject_id, user_id, display_name, avatar_url, metadata）
- Migration: `channel_identity_bind_codes` 表（token, issued_by_user_id, channel_type, expires_at, used_at, used_by_channel_identity_id）
- Migration: `users` 表新增 `source` 字段（默认 'web'）
- Repository 层: ChannelRepo, ChannelIdentityRepo, BindCodeRepo

#### 1b: Channel CRUD API + 前端

- API: `POST/GET/PATCH/DELETE /v1/channels`（含 secrets 表集成，Bot Token 加密存储）
- API: Webhook URL 自动生成逻辑（`{base_url}/v1/channels/{type}/{channel_id}/webhook`）
- Web: SettingsModal 新增 Channels tab（平台选择、Token 输入、状态开关、Webhook URL 复制、Persona 下拉）

#### 1c: 用户绑定

- API: `POST /v1/me/channel-binds`（生成 8 位 bind code，写入 bind_codes 表，24h TTL）
- API: `GET /v1/me/channel-identities`（列出已绑定的平台身份）
- API: `DELETE /v1/me/channel-identities/{id}`（解除绑定）
- Shadow User 创建逻辑: 首次看到平台用户时自动创建 `source='channel_shadow'` 用户 + personal account
- Bind Code 消费逻辑: Bot 侧 /bind 命令触发，事务级 lock + consume + channel_identity.user_id 更新
- Shadow User Memory 迁移: **未实现**（规划项）；当前仅身份与 Thread owner 切换
- Web: SettingsModal 绑定管理区域（已绑定列表、生成 code、解绑）

### Phase 2: Telegram 私聊

#### 2a: Webhook 接收与 Connector

- Telegram Webhook 端点: `POST /v1/channels/telegram/{channel_id}/webhook`（不经 JWT，验签优先）
- 验签实现: `X-Telegram-Bot-Api-Secret-Token` header 校验
- Connector: Telegram Update JSON -> `ChannelIncomingMessage` 统一格式转换
- 命令路由: `/start` (欢迎 + deep link bind 解析), `/help`, `/bind <code>` -> Connector 直接处理不进 Agent
- 幂等处理: 基于 `platform_message_id` 去重，防止 Webhook 重试导致重复处理
- Channel 启用时自动调用 `setWebhook` 注册回调 URL；禁用时调用 `deleteWebhook`
- Bot Commands 注册: 启用时调用 `setMyCommands` 注册 `/start`、`/help`、`/bind`

#### 2b: DM 消息处理 + Thread 映射

- channel_identity upsert: 从 Telegram message.from 提取 user_id/username/first_name，upsert channel_identities
- Shadow User 自动创建: channel_identity.user_id 为空时触发
- DM Thread 查找/创建: 按 `(channel_id, channel_identity_id, persona_id)` 唯一键
- 消息写入: 带结构化 YAML header（channel-identity-id, display-name, channel, conversation-type, time）
- Run 创建 + Job Enqueue: payload 携带 `channel_delivery`（channel_id, channel_type, platform_chat_id, reply_to_message_id, sender_channel_identity_id）

#### 2c: Worker 处理 + 回推

- `mw_channel_context` 中间件: 读取 `channel_delivery`，构建 `ChannelContext`，解析 sender UserID
- MemoryMiddleware 集成: 通过 `ChannelContext.SenderUserID` 加载发送者个人 Memory
- `mw_channel_delivery`（Telegram）: Agent 循环期间 `sendChatAction(typing)`；结束后 `sendMessage`（MarkdownV2、分段、reply/thread）
- `channel_message_deliveries` + `channel_message_ledger` 出站登记；`LookupByPlatformMessage` 供按平台 message id 反查
- `config_json`: `telegram_typing_indicator`、`telegram_reaction_emoji`
- 群聊回复时设置 `reply_to_message_id`

### Phase 3: Telegram 群聊

#### 3a: 被动消息收集

- Group Privacy 配置说明: 用户需在 BotFather 关闭 privacy mode 才能收到所有群聊消息
- 群聊 Thread 查找/创建: 按 `(channel_id, platform_chat_id, persona_id)` 唯一键
- 被动消息写入: 非 @Bot 的群聊消息写入 Thread（带发送者 YAML header），不创建 Run
- 异步写入: 被动消息写入需异步化，避免 Webhook 响应超时（Telegram 要求 60s 内响应）
- 每条被动消息同样触发 channel_identity upsert + shadow user 自动创建

#### 3b: @-mention 触发 + Run

- 触发条件判定: @Bot mention 或 reply to Bot message
- 主动消息写入 Thread（同被动消息格式）+ 创建 Run
- Run 携带 sender 的 UserID（real 或 shadow），Worker 加载该用户的 Memory
- Worker 加载 Thread 历史作为 context（按 `ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS` 裁剪）
- Agent 响应后 ChannelSender 回推到群聊（设置 reply_to_message_id 引用触发消息）

#### 3c: 媒体处理

- 图片/语音/文件/视频: Telegram `getFile` API 下载 -> 上传到 MinIO -> 生成内部 URL
- 媒体作为 attachment 传入 Agent（复用现有 attachment 处理链路）
- Sticker/GIF: 转为图片处理或忽略（按 config_json 配置）
- 语音消息: 如启用 ASR，可选转文字后作为 text 传入

### Phase 4: Context Compaction

#### 4a: 触发与摘要

- Thread 消息量监控: 每次 Run 前检查 Thread 总 token 数是否超过阈值（`ARKLOOP_COMPACT_TRIGGER_THRESHOLD`，默认 2x context budget）
- 压缩范围计算: 保留最近 token budget 内的消息，将更早的消息（+ 已有旧摘要）作为压缩输入
- LLM 摘要调用: 使用低成本模型（建议 GPT-4o-mini / Claude Haiku），prompt 要求保留关键事实、用户偏好、决策结论
- 群聊摘要优化: prompt 额外要求保留每位参与者的身份（display-name）和关键发言

#### 4b: 存储与增量

- `compact_summary` 消息类型: 作为 `role='system', type='compact_summary'` 存入 Thread messages 表
- 替换旧消息: 被压缩的消息标记为 `compacted=true`（不物理删除，保留审计能力），context 加载时跳过
- 增量压缩: 下次触发时，旧 compact_summary + 新溢出消息一起再次压缩为新 summary
- Run context 组装顺序: `[compact_summary] + [recent messages within budget] + [memory context] + [user message]`

#### 4c: 普通 Thread 适配

- DM Thread 同样适用 Compaction（长对话场景）
- Web 端 Thread 同样适用（用户与 Agent 的长期对话）
- Compaction 作为 Pipeline 中间件（或 pre-run hook），对所有 Thread 类型透明

### Phase 5: 平台工具与 Web 集成

#### 5a: Telegram ToolProvider

- `telegram_reply`: 回复引用特定消息（需 message_id）
- `telegram_react`: 给消息添加 emoji reaction
- `telegram_pin`: 置顶/取消置顶消息
- `telegram_send_photo`: 发送图片（从 MinIO 或 URL）
- `telegram_send_document`: 发送文件
- ToolProvider 注册: 当 `RunContext.ChannelContext` 存在且 `ChannelType == "telegram"` 时自动注入
- 工具执行: 通过 `ChannelContext` 获取 chat_id + bot token，直接调用 Telegram Bot API

#### 5b: Web 端 Channel Thread 集成

- Thread 列表: 群聊/DM Thread 在 Web 端可见，标记来源（Telegram/Discord/飞书图标）
- Thread 详情: 完整消息历史（包括被动消息），Run 事件流回放
- 消息渲染: YAML header 解析为发送者标签（显示 display-name + 平台图标）

### Phase 6: 多平台扩展

#### 6a: Discord

- Webhook: Discord Interactions Endpoint URL 配置，Ed25519 签名验证
- Connector: Discord Event JSON -> `ChannelIncomingMessage`
- Slash Commands: 注册 `/bind` 等 Application Commands
- DiscordSender: Markdown 基本兼容，代码块保留，消息上限 2000 字符
- Discord ToolProvider: send_message, add_reaction, pin_message, send_file

#### 6b: 飞书

- Webhook: 飞书开放平台事件订阅配置，SHA256 HMAC 验签
- Connector: 飞书事件 JSON -> `ChannelIncomingMessage`
- FeishuSender: Markdown -> 飞书富文本 JSON（`post` 类型）转换
- 消息卡片: 可选，用于复杂交互场景（按钮、表单）
- Feishu ToolProvider: send_message, reply_message, send_card

#### 6c: 跨平台抽象

- `Connector` 接口统一: 所有平台实现相同的 `Parse(raw) -> ChannelIncomingMessage` + `HandleCommand(cmd)`
- `ChannelSender` 接口统一: `Send(ctx, channelCtx, output) error`
- `Formatter` 接口统一: `Format(markdown) (string, error)` + `MaxMessageLength() int`
- `ToolProvider` 共性工具抽象: `channel_reply`, `channel_react`, `channel_send_file` -> 分派到平台具体实现
- 新平台接入 checklist: 实现 Connector + Sender + Formatter + ToolProvider 四个接口即可

## 16. 配置（新增 env）

| 变量 | 说明 |
|------|------|
| `ARKLOOP_CHANNEL_RATE_LIMIT_PER_MIN` | 每 channel 每分钟最大 Agent 触发数（默认 30） |
| `ARKLOOP_CHANNEL_BIND_CODE_TTL_SEC` | Bind code 有效期（默认 86400，即 24h） |
| `ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS` | 群聊 Run 最大上下文 token 数（默认 16384） |
| `ARKLOOP_CHANNEL_MESSAGE_SEGMENT_DELAY_MS` | 分段消息发送间隔（默认 50） |
| `ARKLOOP_COMPACT_TRIGGER_THRESHOLD` | Compaction 触发阈值（token 数，默认 2x context budget） |

## 17. 开放问题

以下问题在实现过程中需要进一步确认：

1. **Webhook 重试**：平台 Webhook 超时后会重试。API 层需要幂等处理（基于 `platform_message_id` 去重）。
2. **Bot 离线期间的消息**：Bot 不在线时平台会暂存消息。恢复后 Webhook 会集中推送，需要防止雪崩。
3. **被动消息写入性能**：忙碌群聊的被动消息量可能很大。写入 Thread 需要异步化，避免 Webhook 响应超时。
4. **Shadow User 清理**：长期未活跃的 shadow user 是否需要清理策略。当前决定：不清理，Memory 长期保留。
5. **Compaction 模型选择**：用什么模型做摘要压缩。建议用低成本模型（如 GPT-4o-mini），因为摘要不需要创造性。
