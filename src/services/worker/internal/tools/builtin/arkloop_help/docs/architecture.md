# 架构与服务

## 总体形态：单进程嵌入式

Arkloop 的后端是**一个 Go 进程**：**API + Worker + Bridge** 以**库**形式内嵌（`src/services/desktop` 组合，`src/services/api` / `src/services/worker` 根包各自暴露 `StartDesktop` 入口），不再是可独立部署的微服务。

- **存储**：**SQLite**（`shared/database/sqliteadapter`，100+ 个手写 migration，启动时 AutoMigrate）+ **本地 filesystem** 对象存储。
- **事件**：**进程内事件总线**，没有 Redis / pg_notify / 消息队列。
- **多用户**：**单用户**。首次启动写入固定种子用户（`api/internal/auth/desktop_seed.go`），local-only 认证（desktop token 自动注入 + 可选本机密码）。

两种外壳共用这套运行时：

- **Desktop**：Electron 壳（`src/apps/desktop`）内嵌 Go 运行时与打包后的 Web 界面。
- **Headless CLI**：`ark web`（`src/services/cli/cmd/ark`）直接启动运行时并对外提供 Web 界面。默认端口：**Web 19080 / API 19001 / Bridge 19003**（可用 `--port` / `--api-port` / `--bridge-port` 覆盖）。

## 仓库布局（当前）

| 目录 | 内容 |
|------|------|
| `src/services/desktop` | 嵌入式运行时组合（API + Worker + Bridge） |
| `src/services/cli` | `ark` 命令行（headless 入口、聊天、状态等子命令） |
| `src/services/api` / `worker` / `bridge` / `sandbox` | 各领域库代码（`internal/` 下 DDD 风格） |
| `src/services/shared` | 共享库：配置、SQLite 适配器、存储抽象等 |
| `src/apps/desktop` | Electron 桌面壳 |
| `src/apps/web` | Web 对话界面（React 19 / Vite 7 / Tailwind 4） |
| `src/apps/shared` | 前端共享包（API client、主题/语言、UI 组件） |
| `src/personas` | 内置 Persona 模板（`persona.yaml` + prompt） |

Web 开发模式为 Vite（默认 **5173**），将 `/v1` 代理到本地 API（默认 **19001**）。

## Worker 中间件管道（概念顺序）

Worker 由一组**有序中间件**组成，顺序不可随意调整，后序依赖前序写入的 `RunContext` 状态。概念链：

input loading → MCP/tool discovery → persona resolution → **channel context** → **memory/notebook 注入** → routing → context compaction → **tool build** → **agent loop handler**（末端执行对话循环）。

若 **`rc.UserID == nil`**，记忆注入跳过（不注入 `<notebook>` 等块）。

Agent 执行器类型（persona.yaml 的 `executor_type`）：

- **`agent.simple`**：默认，通用推理 + 工具循环（全部内置 persona 用它）。
- **`agent.interactive`**：带用户交互的循环。
- **`task.classify_route`**：分类路由任务。

## 记忆子系统（双轨，相互独立）

### Notebook（默认）

- 纯文本结构化笔记，本地 CRUD，无向量/嵌入，无外部依赖。
- 工具：`notebook_read`、`notebook_write`、`notebook_edit`、`notebook_forget`。
- 注入格式：系统前缀 `<notebook>` 块；条目表 `desktop_memory_entries`，快照表 `user_notebook_snapshots`。

### Nowledge（可选语义记忆）

- 外部 Nowledge 服务，语义检索 + working memory + 会话提炼。
- 配置：`nowledge.base_url` / `nowledge.api_key`（环境变量 `ARKLOOP_NOWLEDGE_BASE_URL` / `ARKLOOP_NOWLEDGE_API_KEY`）。
- 注入：系统前缀 `<working-memory>` 块 + 运行时尾部的召回记忆块。
- 工具：`memory_search`、`memory_read`、`memory_write`、`memory_forget`、`memory_list`、`memory_status`、`memory_context`、`memory_timeline`、`memory_connections`、`memory_thread_search`、`memory_thread_fetch`。

### 运行模式（环境变量级）

| 模式 | 条件 | 注入 | 工具 |
|------|------|------|------|
| 关闭 | `ARKLOOP_MEMORY_ENABLED=false` | 无 | 无 |
| 仅 Notebook（默认） | 未配置 Nowledge | `<notebook>` | notebook_* |
| Notebook + Nowledge | 配置了 Nowledge Base URL | `<notebook>` + `<working-memory>` + 召回块 | 两套兼备 |

记忆归属：绑定 **bot owner（User）**，identity 三元组 `(account_id, user_id, agent_id)`；渠道场景的 UserID 解析见 `channels_telegram` 文档。

## 渠道（Channels）

支持 **Telegram、Discord、QQ、飞书、微信**（QQ/微信经 napcat）。渠道消息与 Web 共用同一条 Worker 管道（channel context → 记忆注入 → 工具构建 → Agent 循环），经 `channel_delivery` 投递回平台。桌面端 Telegram 默认 **getUpdates 长轮询**（无需公网 webhook），也支持配置 webhook。

群聊活跃时由调度器按间隔入队运行（`run_kind=discuss`，状态落 **`scheduled_triggers`** 表）；群聊 run 的 assistant 文本默认不可见，模型须先调用 **`speak`** 工具正文才会发到群里。Persona 还可配置 **heartbeat**（`heartbeat.enabled`，`run_kind=heartbeat`）做定时唤醒。

## 可选模块（compose.yaml）

根目录 `compose.yaml` **只含可选模块**，经 `profiles` 启用，由 Bridge 管理：

- **`docker-sandbox`**：Docker 容器沙箱（sandbox 服务，本机端口 **19002**），代码执行隔离。
- **`searxng`**：自托管元搜索，`web_search` provider。
- **`firecrawl`**：自托管网页抓取，`web_fetch` provider（模块内部自带其专用的 redis/postgres/rabbitmq，与主运行时无关）。

## 平台管理

运行时配置通过 **`platform_manage`** 工具完成（供应商、模型、Agent、Skills、MCP、模块、API key 等），动作清单见 `platform_setup` 文档。

## 与本工具的关系

回答「用的什么数据库」「端口多少」「记忆有几种」「Worker 里谁先谁后」等问题时，应引用本节；具体端口以 `ark web` 启动参数为准。
