# Arkloop

Arkloop 是一个本地优先的对话式 AI agent 平台(个人项目)。desktop 应用开箱即用:单进程内嵌全部后端,SQLite 存储,零外部基础设施。

产品形态:

- **desktop 应用**(Electron,`src/apps/desktop`):主形态,内嵌 web 前端与 Go sidecar
- **ark CLI**(`src/services/cli/cmd/ark`):headless 入口
- **web 前端**(`src/apps/web`):聊天界面,打包进 desktop,也可独立 dev

## Architecture

```
desktop 单进程(src/services/desktop)
  ├─ api(库,loopback :19001)— REST:auth/channels/settings/runs
  ├─ worker(库)— agent 管线:LLM 路由、工具执行、agent loop
  ├─ bridge(库)— 可选模块管理(Docker Compose)
  └─ 内嵌 sandbox(macOS VZ;或外部 sandbox-docker 容器 :19002)

存储:SQLite(进程内,启动时 AutoMigrate)+ filesystem 文件存储
事件:进程内 LocalEventBus(无 Redis/pg_notify)
外部服务:LLM providers(BYOK)、Nowledge(可选,记忆)、可选模块(searxng/firecrawl)
```

仓库是 monorepo:`src/services/`(Go,go.work,Go 1.26)+ `src/apps/`(pnpm monorepo)+ `src/personas/` + `install/` + `compose.yaml`(只剩可选模块)。

## Backend Services

| Service           | 角色                                                                |
| ----------------- | ------------------------------------------------------------------- |
| `api`             | REST API 库(embedded 运行):auth、channels、settings、runs          |
| `worker`          | agent 执行库(embedded):中间件管线、LLM 路由、工具调度、agent loop    |
| `bridge`          | 模块管理库(embedded):可选模块的 Docker Compose 安装/启停           |
| `sandbox`         | 独立容器服务(:19002):Docker sandbox 后端,可选模块,bridge 管理     |
| `desktop`         | embedded 入口:单进程组合 api+worker+bridge+内嵌 sandbox             |
| `shared`          | 共享库:config、sqliteadapter/sqlitepgx、eventbus、objectstore      |
| `cli`             | ark CLI(headless 入口,复用同一 desktop 后端)                       |
| `activity-record` | 独立 sidecar(自包含 SQLite,冻结保留)                               |

服务布局:`cmd/`(入口)+ `internal/`(app/data/http 分层)。注意 api/worker/bridge 没有独立 cmd 入口——它们以库形式被 `desktop` 与 `cli` 嵌入,唯一后端入口是 `src/services/desktop/cmd/desktop`。

### Backend Workflow

```bash
# 构建与测试(单一构建模式,无 build tag)
cd src/services/api && go build ./... && go test ./...
cd src/services/worker && go build ./... && go test ./...

# 运行 desktop 后端(不需要任何外部基础设施)
cd src/services/desktop && go run ./cmd/desktop

# 可选模块(sandbox/searxng/firecrawl)经 desktop 设置页安装,
# 或直接: docker compose --profile docker-sandbox up -d
```

### Key Patterns

- 依赖注入经 `internal/app/` 组合;数据访问走仓储模式(`internal/data/`)
- **中间件管线**:worker 的 run 执行是有序的 RunMiddleware 链(`worker/internal/app/composition.go` 装配):cancel guard → input loader → heartbeat/图像理解/impression/suggestion/sticker 等 prepare → MCP discovery → plugin hooks → channel 系列(admin tag/group merge/telegram/qq tools)→ skill/agent directory/plugin context → memory 注入 → 标题/compact → routing → agent loop。**顺序是有承载语义的**,后来的中间件依赖前面写入 rc 的状态
- **sqlitepgx**:data 层仓储用 pgx 签名编写,desktop 下由 sqlitepgx 在 SQLite 上模拟(pg_notify 等 PG 专属调用会大声报错,不再被静默吞掉)
- **事件**:进程内 LocalEventBus(`shared/eventbus`),topic 常量在 eventbus 包;SSE 只走 bus 单源
- **执行器**:agent.simple / agent.interactive / task.classify_route(persona.yaml 的 `executor_type` 选择;agent.lua 已移除)
- **LLM Heartbeat**:persona.yaml 配置 `heartbeat_enabled`/`heartbeat_interval_minutes`;desktop 调度器(`worker/internal/desktoprun/llm_heartbeat_scheduler.go`)按 interval 入队,worker 以 `run_kind=heartbeat` 执行并注入合成 user 消息,`heartbeat_decision` 工具决定是否发言;状态存 `scheduled_triggers` 表
- **单用户**:固定 UUID 种子(`api/internal/auth/desktop_seed.go`)写入唯一 user + 唯一 personal account + 单行 membership;认证为 local-only(desktop token 自动注入 + 可选本机密码);无注册/邀请/团队/多账户

## Frontend Apps

| App      | 角色                                                                      |
| -------- | ------------------------------------------------------------------------- |
| `web`    | 用户聊天界面(dev :5173,prod 打包进 desktop)                              |
| `desktop`| Electron 壳(主进程、sidecar 生命周期、安装器)                            |
| `shared` | 共享包 `@arkloop/shared`:API client、token storage、theme/locale、UI 组件 |

Tech stack: React 19 / TypeScript 5.9 / Vite 7 / Tailwind CSS 4 / React Router 7

### Frontend Workflow

```bash
pnpm install                    # 仓库根
cd src/apps/web && pnpm dev     # 开发
cd src/apps/web && pnpm build && pnpm lint && pnpm type-check && pnpm test
```

### Key Patterns

- web 经 Vite proxy 把 `/v1` 打到后端
- 状态管理只用 React Context
- 通用组件集中在 `@arkloop/shared/src/components/`(见 rules/frontend/design-system.md)
- i18n 在各 app 的 `src/locales/`,**zh/en 双语必须同步**

## Personas

`src/personas/` 下每个 persona 定义:

- `persona.yaml`:id、工具、budget、temperature、executor_type、heartbeat
- `prompt.md`:system prompt

## Configuration

- 环境变量:`.env`(见 `.env.example`)
- sandbox 模板:`config/sandbox/templates.json`
- Nowledge 记忆:tool provider catalog 的 `memory.nowledge`(API key + base URL)

## Testing

- 单元测试:各服务 `go test ./...`,各 app `pnpm test`
- 已知先存失败测试清单见 CONTRIBUTING 或会话记录;新增失败必须先对账再归因

## Code Conventions

- 提交格式/流程见 `CONTRIBUTING.md`
- Go:显式错误处理,短函数;TS:strict,禁 `any`
- 优先编辑已有文件;依赖经构造器注入
- UI:优先复用 `@arkloop/shared` 已有组件,新通用组件加入 shared,不在业务页自造轮子

## Data Model

单用户模型:一个 User + 一个 personal Account(种子固定 UUID),`account_id` 仍是所有业务表的关联键(projects/threads/runs/messages/api_keys/personas/skills 等),但不存在多租户生命周期。

### Run 工作区

每个 run 可附带独立文件系统(与"团队账户"无关,该概念已删除):

- `workspace_registries`:工作区注册表(workspace_ref, account_id, project_id)
- 文件存储:manifest(`workspaces/{ref}/manifests/{rev}.json`)+ blob(`workspaces/{ref}/blobs/{sha256}`)
- `workspace_skill_enablements`:工作区级 Skill 启用
- API:`GET /v1/workspace-files?run_id=&path=`

### Profile

执行上下文,连接 Account 与 Workspace:

- `profile_registries` / `profile_skill_installs`
- `shared/executionconfig/profile.go`:`PersonaProfile`(prompt, budget)+ `EffectiveProfile`(resolved agent config, token limits, temperature)

### Skills

三层管理:`skill_packages`(包定义)→ `profile_skill_installs`(用户级安装)→ `workspace_skill_enablements`(工作区级启用);ClawHub Registry 外部市场,支持 OpenClaw SKILL.md。

### Memory System

两个**独立子系统**,各自独立的存储、快照、工具集和注入格式,可单独或叠加启用。归属语义:所有记忆绑定 bot owner(User),同一 User 下所有 persona 共享;identity 三元组 `(account_id, user_id, agent_id)`,`agent_id = "user_" + user_id`。

Channel 场景(如 Telegram 群聊)UserID 解析顺序:

1. 消息发送者的 `channel_identity.user_id`(已 `/bind` 的群友)
2. `channels.owner_user_id`(channel 创建者,即 bot owner)

群友不是平台用户,在 bot 记忆里只是 IM 上的 identity(名字 + ID),记忆数据归属 bot owner。

#### 子系统一:Nowledge(语义记忆)

外部 Nowledge 服务,经 tool provider catalog 的 `memory.nowledge` 配置(API key + base URL)。

- 注入格式:`<memory>` XML 块;工具集:`memory_search/read/write/edit/forget`
- 快照表:`user_memory_snapshots`,热路径只读快照不调外部服务

#### 子系统二:Notebook(结构化笔记)

纯文本 CRUD,无向量/嵌入/LLM 处理。

- 注入格式:`<notebook>` XML 块;工具集:`notebook_read/write/edit/forget`
- 快照表:`user_notebook_snapshots`;条目表:`desktop_memory_entries`(SQLite);写入后同步重建快照

#### 隔离与运行模式

两个快照表完全独立,任何情况下不得跨表写入。运行模式由环境变量控制(不在 persona 级):禁用(`ARKLOOP_MEMORY_ENABLED=false`)| notebook(默认)| Nowledge + notebook(配置 memory.nowledge 时叠加)。管线注入在 ChannelContext 之后、Routing 之前,`rc.UserID == nil` 时跳过。

### 关系

```
User(单用户种子)
  -> Account(personal)
     -> Projects -> Personas
     -> Threads -> Runs -> WorkspaceRegistry(run 文件系统)
     -> ProfileRegistry -> ProfileSkillInstalls
     -> WorkspaceSkillEnablements
     -> Channels(Telegram/Discord/QQ/飞书/微信)
```

## Theme System

深浅色经 CSS 变量,三种模式 `system` / `light` / `dark`,由 `data-theme` HTML 属性 + `prefers-color-scheme` 控制。

CSS 变量命名规范(各 app `index.css`):

- 背景:`--c-bg-page` 等;文字:`--c-text-primary` 等;边框:`--c-border` 等;状态:`--c-status-error` 等

写 UI 必须使用 CSS 变量,禁止硬编码颜色。存储 `arkloop:web:theme`(localStorage);`ThemeProvider` + `useTheme()`(from `@arkloop/shared/contexts/theme`)。

## 第一性原理

请使用第一性原理思考。你不能总是假设我非常清楚自己想要什么和该怎
么得到。请保持审慎,从原始需求和问题出发,如果动机和目标不清晰,
停下来和我讨论。

## 方案规范

当需要你给出修改或重构方案时必须符合以下规范:

- 不允许给出兼容性或补丁性的方案
- 不允许过度设计,保持最短路径实现且不能违反第一条要求
- 不允许自行给出我提供的需求以外的方案,例如一些兜底和降级方案,
这可能导致业务逻辑偏移问题
- 必须确保方案的逻辑正确,必须经过全链路的逻辑验证

# 风格类

- 禁止输出 emoji(如 ⭐️🤖🚀)
- 不使用 AI 常用开头语(“Here is the code”, “Certainly” 等)
- 注释必须简短、自然,避免模板化解释
- 类似人类风格的注释,但不要犯人类的错误
- 全程使用中文输出和回答

# 全球开发指南

- 请根据第一性原理,检查有没有过度设计,能不能继续简化,不要写过多的兼容性代码

## 架构原则

### SOLID 原则

- **单一职责原则(SRP)**:一个类应该仅有一个引起其变化的原因
- **开闭原则(OCP)**:对扩展开放,对修改关闭
- **里氏替换原则(LSP)**:子类必须能够替换其基类
- **接口隔离原则(ISP)**:客户端不应该被迫依赖它们不使用的接口
- **依赖倒置原则(DIP)**:依赖于抽象,而不是具体实现

### 设计模式偏好

- 优先使用组合而非继承
- 使用依赖注入以提高可测试性
- 使用仓储模式(Repository Pattern)来分离数据访问逻辑
- 使用策略模式(Strategy Pattern)处理算法变体

## 代码质量

### 命名规范

- 使用清晰、具描述性的变量和函数名
- 避免缩写和魔法数字(magic numbers)
- 遵循项目语言的命名约定

### 代码组织

- 保持文件和函数简洁
- 单个函数不应超过 20 行(复杂逻辑除外)
- 使用有意义的注释解释“为什么”而不是“做什么”

### 错误处理

- 优雅地处理所有可能的错误场景
- 提供有意义的错误信息
- 避免静默失败

## 开发实践

### 通用建议

- **遇事先搜索**:遇到技术问题优先查阅资料
- **测试驱动**:为核心功能编写测试
- **同步文档**:修改代码时更新相关文档
- **安全优先**:始终考虑安全问题,避免硬编码敏感信息

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
- 保持此文件简洁,避免冗余
- 在开始前,请检查项目中是否有Project.md,如果有,请查看

### 一般规范

- 不要使用 emoji

## 文档规范

- 生成的文档(如开发计划、技术方案等)是给LLM用的,不是给人类用的。
- 不得包含开发周期、工期估算、人力投入等人类工作排期相关描述。
- AI是光速运转的硅基生物,不应套用人类的工作节奏预期。

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

1.以暗猜接口为耻,以认真查阅为荣

1. 以模糊执行为耻,以寻求确认为荣

3.以盲想业务为耻,以人类确认为荣
4.以创造接口为耻,以复用现有为荣
5.以跳过验证为耻,以主动测试为荣
6.以破坏架构为耻,以遵循规范为荣
7.以假装理解为耻,以诚实无知为菜
8.以盲目修改为耻,以谨慎重构为荣

在提交commit时,请使用UTC+8时区

在创建 commit 时请遵循 CONTRIBUTING.md 中的提交规范

### 不要在 commit message 中包含 'Co-authored-by' 或任何形式的 AI 署名。

严格的UI/日志约束:
不得将实现说明、持久化描述(例如“保存后生效”)或技术约束作为UI文本标签。
终端日志只能包含数据和错误状态,不能包含功能需求的自然语言描述。
保持界面简洁;偏好标准图标而非冗长的说明文本。
界面和日志细化规则:
无实现说明:不要显示诸如“保存后生效”或“加密存储”等文字。用户通过“保存”按钮或输入类型推断这一点。
仅保留干净日志:后端日志应简洁。不要包含对代码“意图”的自然语言解释(例如,“(骨架模式,无消耗)”)。改用结构化字段。
简约标签:只显示用户采取行动所需的内容。删除任何对开发者来说充当“备忘录”的文本

@rules/frontend/design-system.md

如果需要改业务语义,请先询问用户,而不是自行修改自行理解
如果需要查看代码或者查看数据库,请直接执行,而不是等待确认
请在执行之前先理清用户的需求要做什么

少用 subagent
