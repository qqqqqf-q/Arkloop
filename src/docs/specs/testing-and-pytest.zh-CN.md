# Go 测试策略（可演进）

本文用于约定 Arkloop 的测试目标与最小实践，尽量在”不过早写死细节”和”能落地可执行”之间取得平衡。随着 Tools、Agent Loop、数据库与前端逐步落地，本文会持续补全。

## 1. 为什么要先定测试策略

Arkloop 的核心风险不在 UI，而在”工具执行 + 权限预算 + 审计回放 + 外部依赖（模型/网络）”这条链路。测试的价值是：
- 防止工具越权、超预算、漏审计
- 防止 provider/web 等外部依赖导致回归测试不稳定
- 支撑后续做并行、缓存、重试与 sandbox 加固

## 2. 测试范围（不追求逐字一致）

对 Agent 系统，通常不测”自然语言输出逐字一致”，而测可验证的不变量：
- 结构化输出：是否符合 schema（JSON Schema）
- 工具调用：是否在 allowlist 内、参数是否合规、顺序是否符合约束
- 安全与合规：是否触发正确的拦截/降级/澄清流程
- 成本与预算：是否尊重 tokens/时间/并发等预算上限
- 审计与回放：关键事件是否记录完整，是否能复现一次执行链路

## 3. 分层原则

### 3.0 目录与命令约定

Go 测试文件与被测代码同目录，以 `_test.go` 结尾：
- `src/services/api/internal/http/` — HTTP handler 集成测试
- `src/services/api/internal/data/` — 数据仓储集成测试
- `src/services/api/internal/migrate/` — 迁移集成测试
- `src/services/worker/internal/queue/` — Worker 队列契约测试

常用命令：
- `cd src/services/api && go test ./...` — 只跑单元测试（默认不含 integration tag）
- `cd src/services/api && go test -tags integration ./...` — 跑集成测试（需要 PostgreSQL；本地可用 `docker compose up -d`）
- `cd src/services/worker && go test ./...` — Worker 测试

### 3.1 单元测试（unit）
- 纯逻辑：schema 校验、路由策略、prompt 拼装、技能清单解析、错误分类
- 不触网、不起子进程、不依赖真实数据库
- 目标：快、稳定、可并行，默认进 CI

### 3.2 集成测试（integration）
- API/worker 的边界：鉴权、审计落库、任务编排、tool broker
- 数据层：PostgreSQL 的仓储与迁移回归（核心表、索引、约束与事务边界）
- 使用 `//go:build integration` 构建标签隔离
- 目标：覆盖关键链路，但避免真实外部依赖

### 3.3 端到端测试（e2e）
- 最小纵切：登录/会话/一次工具调用/一次 provider stub 流式输出/审计查询
- 目标：数量少但稳定，保证”系统还能跑”

## 4. 外部依赖：录制/重放优先

对于 provider 与 web_fetch 这类不稳定源，优先做 stub 或 record/replay：
- 默认不在 CI 直连真实模型与公网
- 需要真实连通性时，做成可选的”手动/夜间”任务，并严格隔离密钥与配额

## 5. Tools 怎么测（重点）

Tools 的测试重点是”系统约束”，不是”模型表现”：
- schema：输入/输出严格校验（非法参数必须失败且错误可分类）
- policy：权限不足/预算超限/危险参数必须被拦截（并写入审计）
- sandbox：资源限制生效（超时、最大输出、最大文件、最大网络域）
- 幂等与副作用：声明为无副作用的工具不得偷偷写入；有副作用工具必须可审计
- 可回放：同一 `trace_id` 的事件序列可重放/可对账（至少在测试环境成立）

## 6. 数据库（PostgreSQL）

保持”仓储接口稳定、底层可替换”：
- 业务逻辑依赖仓储接口（Repository），而不是直接写 SQL
- 使用 `pgxpool` 连接池

测试层面：
- 迁移（goose）具备基本回归：能从空库升到最新版本
- 集成测试使用 `testutil.SetupTestDB()` 创建临时测试数据库

## 7. 当前测试覆盖

核心契约的 Go 测试覆盖：
- Auth: `src/services/api/internal/http/v1_auth_integration_test.go`
- Threads: `src/services/api/internal/http/v1_threads_integration_test.go`
- Messages: `src/services/api/internal/http/v1_messages_integration_test.go`
- Runs: `src/services/api/internal/http/v1_runs_integration_test.go`
- SSE/Readyz: `src/services/api/internal/http/readyz_integration_test.go`
- Jobs payload: `src/services/worker/internal/queue/pg_queue_contract_test.go`
- Migrations: `src/services/api/internal/migrate/migrate_integration_test.go`
- Schema repo: `src/services/api/internal/data/schema_repo_integration_test.go`
