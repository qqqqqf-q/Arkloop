# 频道与 Telegram

## 架构角色（概念）

渠道消息由 **API** 接入（Telegram 桌面端默认 **getUpdates 长轮询**，也可配置 webhook），由 **Worker** 执行完整 **Pipeline**（含 `channel_context`、记忆注入、工具构建、Agent 循环），再通过 **`channel_delivery`** 投递回 Telegram 等平台。设计目标是**不为此额外拆服务**：复用与 Web 相同的推理与工具能力。

## Channel 资源模型（摘要）

- **Channel** 归属 **Account**；同一 Account、同一平台通常对应 **一个 Bot 实例**（以产品与实现为准）。
- 支持的频道类型：**`telegram`、`discord`、`qq`、`feishu`、`weixin`**（QQ/微信经 napcat）；本仓库用户问得最多的是 **Telegram**。

## 群聊中的 UserID 与记忆归属（关键）

记忆绑定在 **bot owner（平台 User）** 视角：**切换 Persona 不换记忆主**；同一 User 下多个 Persona **共享**该 User 维度的记忆数据。

Identity 三元组：**`(account_id, user_id, agent_id)`**，其中 **`agent_id = "user_" + user_id`**，实质为 **按用户隔离**；personal account 下 User 与 Account 常表现为 1:1。

**Telegram 群聊**中，当前消息的 **`rc.UserID`** 解析顺序为：

1. 发送者 **`channel_identity.user_id`** —— 适用于已在群内完成 **`/bind`** 等平台绑定 flow 的群友。
2. 否则回落 **`channels.owner_user_id`** —— 即 **频道创建者 / bot owner**。

**群友**可以没有 Arkloop 账号；其在记忆里以 **Telegram 侧身份**（显示名、平台 ID 等）出现，**持久化记忆数据仍归属 bot owner**，而非「每个群友一份独立租户库」。

回答「为什么我和群主看到不同的 notebook/memory」类问题时，要结合 **是否已 bind** 与 **UserID 回落规则** 解释。

## Discuss 与 Heartbeat（群活跃时）

Persona 可配置 **heartbeat**（如仓库 `normal` 模板的 `heartbeat.enabled`）：Telegram **群聊活跃**期间，调度器按间隔入队运行（**`run_kind=discuss`**）；群聊 run 默认 assistant 文本不可见，模型必须先调用 **`speak`**，后续 assistant 正文才会发送到群聊；`speak` 可携带 `reply_to_message_id`。状态写入 **`scheduled_triggers`**。
具体间隔是否配置以**实际使用的 persona.yaml / DB 定义**为准。

## 工具与消息面

Worker 可按渠道注入 **Telegram 相关工具**（如回复、表态等），以 **`channel_delivery`** 载荷与 token 配置为前提；若用户问「群里为什么不能 react」，优先排查 **频道配置、工具白名单、Bot 权限**。

## 与 `arkloop_help` 的配合

当用户问 **Arkloop 是什么、Desktop 是什么、架构如何** 时，应 **`arkloop_help`** 查询 **product_overview / architecture / desktop_guide**；**不要**依赖训练数据猜测（例如把 Desktop 说成 Tauri）。渠道专属「怎么在 APP 里点」结合 **desktop_guide** 与界面实际文案。
