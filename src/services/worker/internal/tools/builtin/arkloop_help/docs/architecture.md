# 架构与服务

## 典型请求路径

```
Client -> Gateway (19000) -> API (19001) -> Worker
                                             |-> LLM（多模型路由）
                                             |-> Sandbox（代码执行）
                                             |-> OpenViking（默认文档端口 19010，语义记忆）
```

Gateway 承担反代、限流、地理与风控等边缘能力；API 负责鉴权、RBAC、计费、迁移与任务调度等；**Worker 无固定对外端口**，由 API 等内部调度执行 Run。

## 后端服务一览（Go）

| 服务 | 对外端口（文档约定） | 职责摘要 |
|------|----------------------|----------|
| `api` | **19001** | 核心 REST、认证、RBAC、计费、迁移、任务入队 |
| `gateway` | **19000** | 反向代理、限流、Geo-IP、风险评分 |
| `worker` | - | Job 执行：LLM 路由、工具分发、Agent 循环、Persona |
| `sandbox` | 实现相关（如容器内 **19002** 等，视 profile） | 隔离代码执行：Linux 生产常见 Firecracker，macOS/Windows 常见 Docker |
| `bridge` | **19003** | 项目桥：Compose 管理、模块注册、审计等 |
| `desktop` | - | **单进程**：API + Worker + Bridge，**SQLite**，无独立 Postgres/Redis 依赖 |
| `shared` | - | 配置、S3 抽象、Redis 工具、积分策略等共享库 |

服务目录惯例：`cmd/` 入口、`internal/` 业务与基础设施（仓储、HTTP、领域逻辑）。

## 前端应用（React / Vite）

| 应用 | Compose 生产端口 | 本地开发 |
|------|------------------|----------|
| `web` | 19080 | Vite 默认 **5173** |
| `console` | 19081 | 本地 dev 口以项目为准 |
| `console-lite` | 19082（compose 默认轻量控制台） | 同上 |
| `developers` | - | 开发者文档/演练界面 |
| `desktop` | - | **Electron** 壳 + 内嵌 Go 侧车（见 desktop 文档） |

技术栈：React 19、TypeScript 5.9、Vite 7、Tailwind CSS 4、React Router 7。

## 基础设施（服务端部署）

常见组合（与 CLAUDE/compose 一致）：

- **PostgreSQL 16**（可通过 **PgBouncer** profile 加速连接，视 `compose.yaml`）
- **Redis 7**
- **对象存储**：**SeaweedFS（S3 profile）** 或 **本地 filesystem**（默认存储后端以环境变量为准）
- **OpenViking**：向量/语义记忆（**openviking** profile，宿主机端口常映射 **19010**）

根目录 **`compose.yaml`** 中的服务名包括：`postgres`、`redis`、`migrate`、`api`、`gateway`、`worker`、`web`、`console-lite`、`console`、`sandbox`（及 firecracker/docker 变体）、`openviking`、`searxng`、`firecrawl` 家族、`bridge`、`seaweedfs`、`pgbouncer` 等。**可选能力通过 `profiles` 打开**（如 `s3`、`pgbouncer`、`openviking`、`firecracker`、`docker-sandbox`、`searxng`、`firecrawl`、`bridge`）。  
若其他文档出现 `performance`、`redis_gateway` 等与当前 `compose.yaml` 不完全一致的 profile 名，**以仓库根目录 `compose.yaml` 实际定义为准**。

## Worker 中间件管道（概念顺序）

Worker 约有 **25 个有序中间件**，顺序**不可随意调整**，后序依赖前序写入的 `RunContext` 状态。文档中的概念链为：

cancel guard → input loading → entitlement → MCP/tool discovery → persona resolution → **channel context** → **memory 注入** → trust/injection scan → routing → context compaction → **tool build** → **agent loop handler**（末端执行对话循环）。

与记忆相关的注入：`MemoryMiddleware` 与 `NotebookInjectionMiddleware` 在 **ChannelContext 之后、Routing 之前**；若 **`rc.UserID == nil`**，两者跳过（不注入 `<memory>` / `<notebook>`）。

## 记忆子系统（双轨）

两者**独立**：快照表、工具、注入 XML 块均不混用。

### OpenViking（Memory）

- 语义检索、分层读取（L0/L1/L2）、可配置自动提炼（distill）。
- 工具：`memory_search`、`memory_read`、`memory_write`、`memory_edit`、`memory_forget`。
- 快照表：`user_memory_snapshots`，主键 `(account_id, user_id, agent_id)`。
- 运行需配置 **`ARKLOOP_OPENVIKING_BASE_URL`**（与 `ARKLOOP_MEMORY_ENABLED` 等配合）。

### Notebook

- 结构化纯文本 CRUD，无向量嵌入。
- 工具：`notebook_read`、`notebook_write`、`notebook_edit`、`notebook_forget`。
- 快照：`user_notebook_snapshots`；条目表服务端为 `notebook_entries`，**Desktop** 为 SQLite 表 **`desktop_memory_entries`**。

### 运行模式（环境变量级）

| 模式 | 条件 | 注入 | 工具 |
|------|------|------|------|
| 关闭 | `ARKLOOP_MEMORY_ENABLED=false` | 无 | 无 |
| 仅 Notebook | 启用且未配 OpenViking Base URL | `<notebook>` | notebook_* |
| Memory + Notebook | 启用且配置了 OpenViking | `<notebook>` + `<memory>` 叠加 | 两套兼备 |

## LLM Heartbeat（群聊场景）

Persona 可配置 `heartbeat_enabled`、`heartbeat_interval_minutes` 等。Telegram **群聊活跃**时，由 **API 侧调度**（`internal/scheduler/`）按间隔入队；Worker 以 `run_kind=heartbeat` 执行并注入**合成用户消息**；工具 **`heartbeat_decision`** 用于模型选择是否回复或附带记忆片段；状态落 **`scheduled_triggers`** 表。

## 数据模型要点（租户）

- **Account** 为租户单元：`personal`（每用户默认一个）与 `workspace`（多成员）。
- **User → Account（personal 1:1；workspace 经 membership N:N）→ Projects / Personas → Threads / Runs**；Workspace 带文件系统注册表与 Skill 启用等（详见官方文档站）。

## Desktop 与 Server 构建

- **默认服务端构建**：`! desktop` 且 **Go 无 desktop tag**：PostgreSQL、pg_notify、Redis 等按需。
- **Desktop 构建**：`-tags desktop`：**SQLite**、进程内事件总线、文件名后缀 `_desktop.go` 的专用实现；**Electron 壳**见桌面文档。

## 与本工具的关系

回答「用的什么数据库」「端口多少」「记忆有两种吗」「Worker 里谁先谁后」等问题时，应引用本节；**具体端口以部署环境变量与 compose 映射为准**，本帮助不替代线上 `docker compose config` 或运维面板。
