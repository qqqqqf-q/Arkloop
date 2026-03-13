# Arkloop

Arkloop is an open-source platform for building conversational AI agents. It provides a managed runtime for LLM-powered agents with built-in tool execution, memory, sandboxed code execution, and multi-model routing.

The codebase is a monorepo split into:

- **Go services** (`src/services/`): Backend microservices
- **Frontend apps** (`src/apps/`): React applications (Vite + TypeScript)
- **Personas** (`src/personas/`): Agent persona templates
- **Infrastructure** (`compose.yaml`): Docker Compose orchestration

## Architecture

```
Client -> Gateway (8000) -> API (8001) -> Worker
                                           |-> LLM (multi-model routing)
                                           |-> Sandbox (8002, code execution)
                                           |-> OpenViking (1933, memory)
```

Infrastructure: PostgreSQL 16 (via PgBouncer) / Redis 7 / MinIO (S3)

## Backend Services

All Go services live under `src/services/` and share a `go.work` workspace (Go 1.26).

| Service | Port | Purpose |
|---------|------|---------|
| `api` | 8001 | Core REST API: auth, RBAC, billing, migrations, job scheduling |
| `gateway` | 8000 | Reverse proxy: rate limiting, geo-IP filtering, risk scoring |
| `worker` | - | Job execution: LLM routing, tool dispatch, agent loop, persona management |
| `sandbox` | 8002 | Code execution: Firecracker VMs (Linux) or Docker containers (macOS/Windows) |
| `shared` | - | Shared libraries: config, S3 abstraction, Redis utils, credit policies |

Each service follows a consistent layout:

```
cmd/          # Entry points (main.go)
internal/     # Private packages (DDD-style: app, data, http, domain logic)
Dockerfile
go.mod
```

### Backend Workflow

```bash
# Start infrastructure
docker compose up -d postgres redis minio pgbouncer

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
- Worker uses a pipeline architecture: routing -> entitlement -> memory -> agent loop
- Worker supports Lua scripting for custom agent logic (`agent.lua` in personas)
- Sandbox supports dual mode: Firecracker (production/Linux) and Docker (dev/macOS)

## Frontend Apps

All frontend apps live under `src/apps/` as a pnpm monorepo.

| App | Port | Purpose |
|-----|------|---------|
| `web` | 5173 | User-facing chat interface |
| `console` | 5174 | Admin dashboard (~35 management pages) |
| `shared` | - | Shared package: API client, token storage, theme/locale contexts |

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
- Shared package (`@arkloop/shared`) provides API client, token management, theme/locale
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

# 风格类
- 禁止输出 emoji（如 ⭐️🤖🚀）
- 不使用 AI 常用开头语（“Here is the code”, “Certainly” 等）
- 注释必须简短、自然，避免模板化解释
- 类似人类风格的注释,但不要犯人类的错误
- 全程使用中文输出和回答

# 全球开发指南
* 请根据第一性原理，检查有没有过度设计，能不能继续简化，不要写过多的兼容性代码
## 架构原则

### SOLID 原则

* **单一职责原则（SRP）**：一个类应该仅有一个引起其变化的原因
* **开闭原则（OCP）**：对扩展开放，对修改关闭
* **里氏替换原则（LSP）**：子类必须能够替换其基类
* **接口隔离原则（ISP）**：客户端不应该被迫依赖它们不使用的接口
* **依赖倒置原则（DIP）**：依赖于抽象，而不是具体实现

### 设计模式偏好

* 优先使用组合而非继承
* 使用依赖注入以提高可测试性
* 使用仓储模式（Repository Pattern）来分离数据访问逻辑
* 使用策略模式（Strategy Pattern）处理算法变体

## 代码质量

### 命名规范

* 使用清晰、具描述性的变量和函数名
* 避免缩写和魔法数字（magic numbers）
* 遵循项目语言的命名约定

### 代码组织

* 保持文件和函数简洁
* 单个函数不应超过 20 行（复杂逻辑除外）
* 使用有意义的注释解释“为什么”而不是“做什么”

### 错误处理

* 优雅地处理所有可能的错误场景
* 提供有意义的错误信息
* 避免静默失败

## 开发实践

### 通用建议

* **遇事先搜索**：遇到技术问题优先查阅资料
* **测试驱动**：为核心功能编写测试
* **同步文档**：修改代码时更新相关文档
* **安全优先**：始终考虑安全问题，避免硬编码敏感信息

### 性能考量

* 避免过早优化
* 注重算法复杂度
* 合理使用缓存
* 监控内存使用

### 代码审查

* 注重代码的可读性和可维护性
* 检查边界条件的处理
* 验证错误处理逻辑
* 确保测试覆盖

### 包管理与项目标准

* Node.js 项目优先使用 pnpm
* 提交规范请查看 CONTRIBUTING.md 中的 Commits 段落
* 保持此文件简洁，避免冗余
* 在开始前，请检查项目中是否有Project.md,如果有,请查看
### 一般规范

* 不要使用 emoji

## 文档规范
- 生成的文档（如开发计划、技术方案等）是给LLM用的，不是给人类用的。
- 不得包含开发周期、工期估算、人力投入等人类工作排期相关描述。
- AI是光速运转的硅基生物，不应套用人类的工作节奏预期。

### 额外规范
* 不要创建无意义/没有实际作用的文件/文件夹

* 项目应基于Linux/Windows/MacOS三段运行

* 模块应自上而下注入

* 请不要简化问题,简化操作
* 不要试图偷懒/摸鱼,保持代码质量
* 回答问题不要一味的求和用户,给出优质的答案才是你要干的事

* 你要写的是一个企业级的大型项目,应该遵守规范
* 你应该像Linus一样,保持代码的整洁性/可读性/维护性

* 不准空写代码或者直接使用Pass等函数直接跳过(除非用户需要)
* 禁止简化代码和需求,例如用简单逻辑跳过或使用虚假数据

* 不要写不必要的代码和文件

* 严格遵循此文件

1.以暗猜接口为耻，以认真查阅为荣
2. 以模糊执行为耻，以寻求确认为荣
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
