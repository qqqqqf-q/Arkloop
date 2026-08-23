# Arkloop

Arkloop is an open-source platform for building conversational AI agents. It provides a managed runtime for LLM-powered agents with built-in tool execution, memory, sandboxed code execution, and multi-model routing.

The codebase is a monorepo split into:

- **Go services** (`src/services/`): Backend microservices
- **Frontend apps** (`src/apps/`): React applications (Vite + TypeScript)
- **Personas** (`src/personas/`): Agent persona templates
- **Infrastructure** (`compose.yaml`): Docker Compose orchestration

## Architecture

```
Client -> Gateway (19000) -> API (19001) -> Worker
                                             |-> LLM (multi-model routing)
                                             |-> Sandbox (code execution)
                                             |-> OpenViking (19010, memory)
```

Infrastructure: PostgreSQL 16 (via PgBouncer) / Redis 7 / SeaweedFS (S3) 或 filesystem (默认)

## Backend Services

All Go services live under `src/services/` and share a `go.work` workspace (Go 1.26).


| Service   | Port  | Purpose                                                                         |
| --------- | ----- | ------------------------------------------------------------------------------- |
| `api`     | 19001 | Core REST API: auth, RBAC, billing, migrations, job scheduling                  |
| `gateway` | 19000 | Reverse proxy: rate limiting, geo-IP filtering, risk scoring                    |
| `worker`  | -     | Job execution: LLM routing, tool dispatch, agent loop, persona management       |
| `sandbox` | -     | Code execution: Firecracker VMs (Linux) or Docker containers (macOS/Windows)    |
| `bridge`  | 19003 | Project bridge: Docker Compose management, module registry, audit logging       |
| `desktop` | -     | Desktop embedded build: API + Worker + Bridge in one process (SQLite, no infra) |
| `shared`  | -     | Shared libraries: config, S3 abstraction, Redis utils, credit policies          |


Each service follows a consistent layout:

```
cmd/          # Entry points (main.go)
internal/     # Private packages (DDD-style: app, data, http, domain logic)
Dockerfile
go.mod
```

### Backend Workflow

```bash
# Start infrastructure (use --profile to include optional layers)
docker compose up -d postgres redis
docker compose --profile performance up -d pgbouncer redis_gateway  # performance layer
docker compose --profile s3 up -d seaweedfs                        # S3 storage (also used by Bridge)

# Run migrations
cd src/services/api && go run ./cmd/migrate

# Run a service
cd src/services/api && go run ./cmd/api
cd src/services/gateway && go run ./cmd/gateway
cd src/services/worker && go run ./cmd/worker

# Tests
cd src/services/api && go test ./...
cd src/services/worker && go test ./...
```

### Key Patterns

- Services use dependency injection via composition in `internal/app/`
- Database access through repository pattern (`internal/data/`)
- Worker uses a middleware pipeline architecture (~25 ordered middlewares): cancel guard → input loading → entitlement → MCP/tool discovery → persona resolution → channel context → memory injection → trust/injection scan → routing → context compaction → tool build → agent loop handler. Order is load-bearing (each middleware may depend on state written by earlier ones).
- Worker supports Lua scripting for custom agent logic (`agent.lua` in personas)
- Sandbox supports dual mode: Firecracker (production/Linux) and Docker (dev/macOS)
- Worker has two build targets: the default server build (`!desktop`) and a desktop build (`desktop` tag), sharing the same pipeline but differing in infrastructure dependencies (e.g., SQLite vs PostgreSQL, in-process event bus vs pg_notify). Desktop-specific files use the `_desktop.go` suffix.
- LLM Heartbeat: personas can configure periodic heartbeat runs (`heartbeat_enabled`, `heartbeat_interval_minutes` in `persona.yaml`). When a Telegram group chat is active, the scheduler (API side, `internal/scheduler/`) enqueues heartbeat jobs at the configured interval. The worker executes them with `run_kind=heartbeat`, injecting a synthetic user message. The `heartbeat_decision` tool lets the LLM choose to reply silently or include memory fragments. Heartbeat state is persisted in the `scheduled_triggers` table.

## Frontend Apps

All frontend apps live under `src/apps/` as a pnpm monorepo.


| App            | Port              | Purpose                                                                  |
| -------------- | ----------------- | ------------------------------------------------------------------------ |
| `web`          | 19080 (dev: 5173) | User-facing chat interface                                               |
| `console`      | 19081             | Admin dashboard (~35 management pages)                                   |
| `console-lite` | 19082             | Admin dashboard (lightweight, default in compose)                        |
| `developers`   | -                 | Developer-facing playground/debug interface                              |
| `desktop`      | -                 | Desktop app entry point (Electron or similar)                            |
| `shared`       | -                 | Shared package: API client, token storage, theme/locale, auth components |


Note: 19080/19081/19082 are Docker Compose production ports. Local development uses Vite default port 5173.

Tech stack: React 19 / TypeScript 5.9 / Vite 7 / Tailwind CSS 4 / React Router 7

### Frontend Workflow

```bash
# Install dependencies (from repo root)
pnpm install

# Development
cd src/apps/web && pnpm dev
cd src/apps/console && pnpm dev

# Build & check
cd src/apps/web && pnpm build
cd src/apps/web && pnpm lint
cd src/apps/web && pnpm type-check

# Tests
cd src/apps/web && pnpm test
```

### Key Patterns

- Both apps proxy `/v1` API requests to the backend via Vite dev server config
- State management via React Context only (no Redux/Zustand)
- Shared package (`@arkloop/shared`) provides API client (`apiFetch`), in-memory token storage, theme/locale context factories (`ThemeProvider`/`useTheme`, `createLocaleContext`), and UI components (`AuthPage`, `BootstrapPage`, `SettingsModal`, `Toast`, `ErrorCallout`, `Turnstile`)
- i18n translations live in each app's `src/locales/`

## Personas

Agent persona templates under `src/personas/`. Each persona defines:

- `persona.yaml`: Configuration (id, tools, budget, temperature)
- `prompt.md`: System prompt
- `agent.lua` (optional): Custom agent loop logic

## Configuration

- Environment: `.env` file (see `.env.example` for all variables)
- Sandbox templates: `config/sandbox/templates.json`
- OpenViking: `config/openviking/ov.conf`

## Testing

- **Unit tests**: `go test ./...` per service, `pnpm test` per app
- **Smoke tests**: `tests/smoke/` (CI-only, requires full stack)
- **Benchmarks**: `tests/bench/` (load testing against running instance)

## Code Conventions

- Follow `CONTRIBUTING.md` for commit format, code style, and PR process
- Go: standard conventions, explicit error handling, short focused functions
- TypeScript: strict mode, ESLint, no `any` types
- Prefer editing existing files over creating new ones
- Dependencies inject through constructors; respect clean architecture boundaries
- UI: 优先复用 `@arkloop/shared` 中已有组件（Decision Token 体系）；需要新增通用组件时应将其加入 `src/apps/shared/src/components/`，不在业务页面中自造轮子

## Data Model

Organization 概念已移除 (migration 00118)，Account 是唯一的租户单元。

### Account

Account 是核心多租户单元，有两种类型:

- `personal`: 每个用户自动创建一个，作为默认账户
- `workspace`: 多用户共享的工作区账户

关键表: `accounts`, `account_memberships`
所有业务表通过 `account_id` 关联 (projects, threads, runs, messages, api_keys, personas, skills, credits 等)

### Workspace

Workspace 是 type="workspace" 的 Account，附带文件系统:

- `workspace_registries`: 工作区注册表 (workspace_ref, account_id, project_id)
- `workspace_skill_enablements`: 工作区级 Skill 启用
- 文件系统: manifest (`workspaces/{ref}/manifests/{rev}.json`) + blob (`workspaces/{ref}/blobs/{sha256}`)
- API: `GET /v1/workspace-files?run_id=&path=`

### Profile

Profile 是执行上下文，连接 Account 和 Workspace:

- `profile_registries`: Profile 注册表 (profile_ref, account_id, default_workspace_ref)
- `profile_skill_installs`: Profile 级 Skill 安装 (profile_ref, skill_key, version)
- `shared/executionconfig/profile.go`: `PersonaProfile` (prompt, budget) + `EffectiveProfile` (resolved agent config, token limits, temperature)

### Skills

三层 Skill 管理:

- `skill_packages`: Skill 包定义 (account_id, skill_key, version)
- `profile_skill_installs`: Profile 安装 (用户级)
- `workspace_skill_enablements`: Workspace 启用 (团队级)
- ClawHub Registry: 外部 Skill 市场，支持 OpenClaw SKILL.md 格式

### Memory System

Memory 系统包含两个**独立子系统**，各自有独立的存储、快照、工具集和注入格式。两者可以单独使用，也可以同时启用叠加注入。

归属语义：所有记忆绑定在 bot owner（User）上，从 bot 的视角看世界。bot 属于平台上的某个 User，切换 persona 不改变记忆归属，同一 User 下所有 persona 共享同一份记忆。

identity 三元组: `(account_id, user_id, agent_id)`，其中 `agent_id = "user_" + user_id`，实质上是 per-user 隔离。personal account 下 User 与 Account 1:1，等同于 per-account。

Channel 场景（Telegram 群聊）的 UserID 解析顺序:

1. 消息发送者的 `channel_identity.user_id`（已 `/bind` 的群友）
2. `channels.owner_user_id`（channel 创建者，即 bot owner）

群友不是平台用户，不需要 Arkloop 账号。群友在 bot 的记忆里只是 Telegram 上的 identity（名字 + ID），记忆数据本身归属 bot owner。

#### 子系统一：Memory（OpenViking）

语义记忆，依赖外部 OpenViking 服务，提供向量检索、分层内容读取（L0/L1/L2）、自动会话提炼（distill）。

- 注入格式: `<memory>` XML 块
- 工具集: `memory_search`, `memory_read`, `memory_write`, `memory_edit`, `memory_forget`
- 快照表: `user_memory_snapshots`，PK `(account_id, user_id, agent_id)`，热路径只读快照不调用 OpenViking
- 写入后异步刷新快照（调 OpenViking Find 重建，最多重试 30 次，间隔 10s）
- 自动提炼由 `memory.distill_enabled` 配置项控制

#### 子系统二：Notebook

结构化笔记，纯文本 CRUD，无向量/嵌入/LLM 处理。

- 注入格式: `<notebook>` XML 块
- 工具集: `notebook_read`, `notebook_write`, `notebook_edit`, `notebook_forget`
- 快照表: `user_notebook_snapshots`，PK `(account_id, user_id, agent_id)`
- 条目表: 服务端 `notebook_entries`（PostgreSQL），Desktop `desktop_memory_entries`（SQLite）
- 写入后同步重建快照

#### 两个子系统严格隔离

`user_memory_snapshots` 和 `user_notebook_snapshots` 是**完全独立的表**，存储各自的快照内容（`memory_block` vs `notebook_block`）。任何情况下不得将一个子系统的快照数据写入另一个子系统的快照表。

#### 运行模式

由环境变量控制，不在 persona 级别配置:


| 模式                | 条件                                 | 注入内容                         | 工具集                   |
| ----------------- | ---------------------------------- | ---------------------------- | --------------------- |
| disabled          | `ARKLOOP_MEMORY_ENABLED=false`     | 无                            | 无                     |
| notebook-only     | 启用，无 `ARKLOOP_OPENVIKING_BASE_URL` | `<notebook>`                 | notebook_*            |
| memory + notebook | 启用，有 `ARKLOOP_OPENVIKING_BASE_URL` | `<notebook>` + `<memory>` 叠加 | notebook_* + memory_* |


Pipeline 注入位置: MemoryMiddleware (#15) + NotebookInjectionMiddleware (#16)，均在 ChannelContext (#7) 之后、Routing (#19) 之前。`rc.UserID == nil` 时两者均跳过。两个 middleware 各自独立向 `rc.SystemPrompt` 追加内容。

### 关系

```
User
  -> Account (personal, 1:1)
  -> Account (workspace, N:N via account_memberships)
     -> Projects -> Personas
     -> Threads -> Runs -> WorkspaceRegistry (file system)
     -> ProfileRegistry -> ProfileSkillInstalls
     -> WorkspaceSkillEnablements
```

## Theme System

深浅色通过 CSS 变量实现，三种模式: `system` / `light` / `dark`

机制: `data-theme` HTML 属性 + `prefers-color-scheme` media query

- 不设置 `data-theme` = 跟随系统
- `data-theme="light"` = 强制浅色
- `data-theme="dark"` = 强制深色

CSS 变量命名规范 (在 `index.css` 中定义):

- 背景: `--c-bg-page`, `--c-bg-sidebar`, `--c-bg-deep`, `--c-bg-sub`, `--c-bg-input`
- 文字: `--c-text-primary`, `--c-text-secondary`, `--c-text-tertiary`, `--c-text-muted`
- 边框: `--c-border`, `--c-border-subtle`, `--c-border-mid`
- 状态: `--c-status-error`, `--c-status-warning`, `--c-status-success`

Web 有 80+ CSS 变量，Console 有 44+ CSS 变量。写 UI 时必须使用 CSS 变量而非硬编码颜色值。

存储: `arkloop:web:theme` / `arkloop:console:theme` (localStorage)
Context: `ThemeProvider` + `useTheme()` (from `@arkloop/shared/contexts/theme`)

## 第一性原理

请使用第一性原理思考。你不能总是假设我非常清楚自己想要什么和该怎
么得到。请保持审慎，从原始需求和问题出发，如果动机和目标不清晰，
停下来和我讨论。

## 方案规范

当需要你给出修改或重构方案时必须符合以下规范：

- 不允许给出兼容性或补丁性的方案
- 不允许过度设计，保持最短路径实现且不能违反第一条要求
- 不允许自行给出我提供的需求以外的方案，例如一些兜底和降级方案，
这可能导致业务逻辑偏移问题
- 必须确保方案的逻辑正确，必须经过全链路的逻辑验证

# 风格类

- 禁止输出 emoji（如 ⭐️🤖🚀）
- 不使用 AI 常用开头语（“Here is the code”, “Certainly” 等）
- 注释必须简短、自然，避免模板化解释
- 类似人类风格的注释,但不要犯人类的错误
- 全程使用中文输出和回答

# 全球开发指南

- 请根据第一性原理，检查有没有过度设计，能不能继续简化，不要写过多的兼容性代码

## 架构原则

### SOLID 原则

- **单一职责原则（SRP）**：一个类应该仅有一个引起其变化的原因
- **开闭原则（OCP）**：对扩展开放，对修改关闭
- **里氏替换原则（LSP）**：子类必须能够替换其基类
- **接口隔离原则（ISP）**：客户端不应该被迫依赖它们不使用的接口
- **依赖倒置原则（DIP）**：依赖于抽象，而不是具体实现

### 设计模式偏好

- 优先使用组合而非继承
- 使用依赖注入以提高可测试性
- 使用仓储模式（Repository Pattern）来分离数据访问逻辑
- 使用策略模式（Strategy Pattern）处理算法变体

## 代码质量

### 命名规范

- 使用清晰、具描述性的变量和函数名
- 避免缩写和魔法数字（magic numbers）
- 遵循项目语言的命名约定

### 代码组织

- 保持文件和函数简洁
- 单个函数不应超过 20 行（复杂逻辑除外）
- 使用有意义的注释解释“为什么”而不是“做什么”

### 错误处理

- 优雅地处理所有可能的错误场景
- 提供有意义的错误信息
- 避免静默失败

## 开发实践

### 通用建议

- **遇事先搜索**：遇到技术问题优先查阅资料
- **测试驱动**：为核心功能编写测试
- **同步文档**：修改代码时更新相关文档
- **安全优先**：始终考虑安全问题，避免硬编码敏感信息

### 性能考量

- 避免过早优化
- 注重算法复杂度
- 合理使用缓存
- 监控内存使用

### 代码审查

- 注重代码的可读性和可维护性
- 检查边界条件的处理
- 验证错误处理逻辑
- 确保测试覆盖

### 包管理与项目标准

- Node.js 项目优先使用 pnpm
- 提交规范请查看 CONTRIBUTING.md 中的 Commits 段落
- 保持此文件简洁，避免冗余
- 在开始前，请检查项目中是否有Project.md,如果有,请查看

### 一般规范

- 不要使用 emoji

## 文档规范

- 生成的文档（如开发计划、技术方案等）是给LLM用的，不是给人类用的。
- 不得包含开发周期、工期估算、人力投入等人类工作排期相关描述。
- AI是光速运转的硅基生物，不应套用人类的工作节奏预期。

### 额外规范

- 不要创建无意义/没有实际作用的文件/文件夹
- 项目应基于Linux/Windows/MacOS三段运行
- 模块应自上而下注入
- 请不要简化问题,简化操作
- 不要试图偷懒/摸鱼,保持代码质量
- 回答问题不要一味的求和用户,给出优质的答案才是你要干的事
- 你应该像Linus一样,保持代码的整洁性/可读性/维护性
- 不准空写代码或者直接使用Pass等函数直接跳过(除非用户需要)
- 禁止简化代码和需求,例如用简单逻辑跳过或使用虚假数据
- 不要写不必要的代码和文件
- 严格遵循此文件

1.以暗猜接口为耻，以认真查阅为荣

1. 以模糊执行为耻，以寻求确认为荣

3.以盲想业务为耻，以人类确认为荣
4.以创造接口为耻，以复用现有为荣
5.以跳过验证为耻，以主动测试为荣
6.以破坏架构为耻，以遵循规范为荣
7.以假装理解为耻，以诚实无知为菜
8.以盲目修改为耻，以谨慎重构为荣

在提交commit时，请使用UTC+8时区

在创建 commit 时请遵循 CONTRIBUTING.md 中的提交规范

### 不要在 commit message 中包含 'Co-authored-by' 或任何形式的 AI 署名。

严格的UI/日志约束：
不得将实现说明、持久化描述（例如“保存后生效”）或技术约束作为UI文本标签。
终端日志只能包含数据和错误状态，不能包含功能需求的自然语言描述。
保持界面简洁;偏好标准图标而非冗长的说明文本。
界面和日志细化规则：
无实现说明：不要显示诸如“保存后生效”或“加密存储”等文字。用户通过“保存”按钮或输入类型推断这一点。
仅保留干净日志：后端日志应简洁。不要包含对代码“意图”的自然语言解释（例如，“（骨架模式，无消耗）”）。改用结构化字段。
简约标签：只显示用户采取行动所需的内容。删除任何对开发者来说充当“备忘录”的文本

@rules/backend/agent-workflow.md
@rules/frontend/agent-workflow.md
@rules/frontend/design-system.md

如果需要改业务语义,请先询问用户,而不是自行修改自行理解
如果需要查看代码或者查看数据库,请直接执行,而不是等待确认
请在执行之前先理清用户的需求要做什么

少用 subagent
