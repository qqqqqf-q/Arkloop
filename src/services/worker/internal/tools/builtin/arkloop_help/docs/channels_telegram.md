# 频道与 Telegram

## 架构角色（概念）

渠道消息一般由 **Gateway → API**（如 webhook）入队，由 **Worker** 执行完整 **Pipeline**（含 `channel_context`、记忆注入、工具构建、Agent 循环），再通过 **`channel_delivery`** 投递回 Telegram 等平台。设计目标是**不为此额外拆微服务**：复用与 Web 相同的推理与工具能力。

开发者规格文档（仓库内 `src/apps/developers/content/docs/.../channel-integration-architecture.md`）对拓扑与资源模型有更细字段说明；本帮助只保留答疑常用事实。

## Channel 资源模型（摘要）

- **Channel** 归属 **Account**；同一 Account、同一平台通常对应 **一个 Bot 实例**（以产品与实现为准）。
- 频道类型在规格中可包含 **`telegram`、`discord`、`feishu`** 等；本仓库用户问得最多的是 **Telegram**。

## 群聊中的 UserID 与记忆归属（关键）

记忆绑定在 **bot owner（平台 User）** 视角：**切换 Persona 不换记忆主**；同一 User 下多个 Persona **共享**该 User 维度的记忆数据。

Identity 三元组：**`(account_id, user_id, agent_id)`**，其中 **`agent_id = "user_" + user_id`**，实质为 **按用户隔离**；personal account 下 User 与 Account 常表现为 1:1。

**Telegram 群聊**中，当前消息的 **`rc.UserID`** 解析顺序为：

1. 发送者 **`channel_identity.user_id`** —— 适用于已在群内完成 **`/bind`** 等平台绑定 flow 的群友。
2. 否则回落 **`channels.owner_user_id`** —— 即 **频道创建者 / bot owner**。

**群友**可以没有 Arkloop 账号；其在记忆里以 **Telegram 侧身份**（显示名、平台 ID 等）出现，**持久化记忆数据仍归属 bot owner**，而非「每个群友一份独立租户库」。

回答「为什么我和群主看到不同的 notebook/memory」类问题时，要结合 **是否已 bind** 与 **UserID 回落规则** 解释。

## Heartbeat（群活跃时）

部分 Persona（如仓库 `normal` 模板）启用 **`heartbeat`**：Telegram **群聊活跃**期间，API 侧调度器按 **`heartbeat_interval_minutes`** 等配置入队 **heartbeat Run**；Worker 中 `run_kind=heartbeat`，带 **合成用户消息**；工具 **`heartbeat_decision`** 决定静默或回复、是否附带记忆片段；状态写入 **`scheduled_triggers`**。  
具体间隔是否配置以**实际使用的 persona.yaml / DB 定义**为准。

## 工具与消息面

Worker 可按渠道注入 **Telegram 相关工具**（如回复、表态等），以 **`channel_delivery`** 载荷与 token 配置为前提；若用户问「群里为什么不能 react」，优先排查 **频道配置、工具白名单、Bot 权限**。

## 与 `arkloop_help` 的配合

当用户问 **Arkloop 是什么、Desktop 是什么、架构如何** 时，应 **`arkloop_help`** 查询 **product_overview / architecture / desktop_guide**；**不要**依赖训练数据猜测（例如把 Desktop 说成 Tauri）。渠道专属「怎么在 APP 里点」结合 **desktop_guide** 与界面实际文案。
