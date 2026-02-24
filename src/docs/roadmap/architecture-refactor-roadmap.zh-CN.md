# 架构重构路线图（从审计报告到目标架构）

本文是 Arkloop 架构重构的薄片式执行路线。每个薄片（Slice）可独立验收，按 Phase 分组推进。

关联文档：
- 审计报告：`src/docs/architecture/architecture-problems.zh-CN.md`
- 目标架构：`src/docs/architecture/architecture-design-v2.zh-CN.md`
- 开发路线：`src/docs/roadmap/development-roadmap.zh-CN.md`

## 0. 当前代码基线

重构的起点是已完成的仓库现状，不能脱离实际代码凭空设计：

**API 服务（`src/services/api/`）：**
- Go `net/http`，11 个 goose migration（截止 `00011`）
- 表：`orgs`（3 字段）、`users`（3 字段 + `tokens_invalid_before`）、`user_credentials`（login/password_hash）、`org_memberships`（role TEXT）、`threads`、`messages`（content TEXT）、`runs`（status TEXT 无 CHECK）、`run_events`（seq 通过行级锁分配）、`audit_logs`、`jobs`
- 鉴权：JWT Bearer + `tokens_invalid_before` 全局失效策略
- 端点：auth（login/register/refresh/logout/me）、threads CRUD、messages CRUD、runs（create/cancel/get/list/events SSE）

**Worker 服务（`src/services/worker/`）：**
- `consumer/loop.go`：`SKIP LOCKED` 消费 + heartbeat + advisory lock 去重
- `runengine/v1.go`：Agent Loop 多轮执行，200ms 批提交 + LISTEN/NOTIFY 取消信号
- `routing/config.go`：`ARKLOOP_PROVIDER_ROUTING_JSON` 环境变量加载凭证/路由
- `mcp/`：stdio 传输 + 内存连接池，配置从 `ARKLOOP_MCP_CONFIG_FILE` 加载
- `skills/`：从文件系统 `src/skills/` 加载 `skill.yaml + prompt.md`
- `tools/`：内置工具（echo/noop/web_search/web_fetch）+ MCP 工具

**基础设施：**
- `compose.yaml`：仅 PostgreSQL 16-alpine
- 无 Redis、无对象存储、无 Gateway、无可观测性

## 1. 重构入手策略

审计报告列出了 7 类 P0 + 10 类 P1 + 11 类 P2 问题。不能同时开工。入手原则如下：

**先修地基，再盖楼：**
- 数据库 schema 的缺陷（users 缺 email、runs 缺生命周期字段、run_events 行锁热点）是所有上层功能的前提。schema 改错了后续全部返工。
- Redis 是 Gateway 限流、Worker 分布式锁、SSE 跨实例广播的共同依赖。不引入 Redis，Gateway 和实时推送改造都做不了。

**先内后外：**
- 先修复数据模型和内部契约（schema + Worker 事件写入路径），再做外部暴露面（Gateway、Webhooks、API Keys）。
- 先修 Worker 执行路径的结构性问题（MCP 远端化、Skills 入库、凭证入库），再做 Console 管理界面。

**先阻塞后增强：**
- P0 问题（阻塞扩展）优先于 P1（功能缺失）优先于 P2（规模化后暴露）。
- 但同一 Phase 内按依赖关系排列，而不是严格按 P0/P1/P2 排。

## 2. Phase 总览

| Phase | 主题 | 核心目标 | 涉及审计问题 |
|---|---|---|---|
| Phase 1 | 数据模型修正 | 修复 schema 的结构性缺陷，为后续所有功能打地基 | P0: users 缺 email; P0: run_events 行锁; P1: runs 缺生命周期字段; P1: messages 不支持多模态; P1: 无软删除; P1: runs.status 不可信 |
| Phase 2 | 基础设施引入 | 引入 Redis + 对象存储，消除单点依赖 | P0: 没有 Redis |
| Phase 3 | Worker 执行路径重构 | MCP 远端化 + Skills 入库 + 凭证入库 + 事件写入优化 | P0: MCP stdio 绑定; P0: Worker 调度盲目; P1: Skills 绑定文件系统; P1: LLM 凭证锁死 env var; P1: SSE 双重延迟 |
| Phase 4 | Gateway + 安全基础 | 独立 Gateway 层 + secrets 统一管理 + API Keys | P0: 没有 Gateway 层; P1: 没有 secrets 表; P2: 没有 API Key 管理 |
| Phase 4.5 | Worker 执行 Pipeline | 将 EngineV1.Execute 重构为 Middleware Pipeline，建立可插拔的执行阶段架构 | 无直接审计对应；解决 Execute 可扩展性瓶颈 |
| Phase 5 | 组织模型与权限 | org 邀请 + RBAC + teams/projects 层级 | P1: 没有 org 邀请; P2: RBAC 过于简陋; P2: 没有 projects/teams 层级 |
| Phase 6 | 企业级能力 | Webhooks + Agent Config + 订阅/权益/用量 + 通知 + Feature Flags | P1: 没有 Webhooks; P1: 审计日志缺 IP; P2: 没有通知系统; P2: 没有 Feature Flags; 六「订阅与计费不存在」 |
| Phase 6.5 | Console Management | 为 Phase 1-6 全部后端功能提供 Web 管理界面，补全 Audit List / 全局 Runs List 端点 | 无直接审计对应；解决「后端功能无法通过 UI 管理」的操作面缺失 |
| Phase 7 | 性能与可扩展性 | PgBouncer + run_events 分区 + SSE 多实例广播 + 并发计数器修复 | 九.9.1: PgBouncer 盲点; 九.9.2: run_events 无分区; 九.9.3: SSE 多实例缺失; 九.9.5: 计数器泄漏 |
| Phase 8 | 公开发布基础 | Dashboard + 用户管理 + 邀请码推广 + 兑换码 + 积分体系 + 用量跟踪增强 + 通知广播 | 「后台无数据可看」「无病毒式推广机制」「无滥用防护」「通知无广播能力」 |

---

## 3. Phase 1 -- 数据模型修正

目标：修复数据库 schema 的结构性缺陷。这是整个重构的地基，不碰业务代码逻辑，只做 migration + repository 层适配。

### R10 -- users 表补全（email + status + soft delete）

- **关联审计**：二.2.1「用户身份是残缺的」；P0「users 表没有 email」
- **关联目标架构**：六.6.1 `ALTER TABLE users ADD COLUMN email/status/deleted_at/...`
- **目标**：`users` 表增加 `email`、`email_verified_at`、`status`、`deleted_at`、`avatar_url`、`locale`、`timezone`、`last_login_at`。现有数据不受影响（全部 nullable 或带默认值）。
- **关键点**：
  - `email` 先 nullable（现有用户没有 email），后续注册流程再改为必填。
  - `status` 默认 `'active'`，CHECK 约束 `('active', 'suspended', 'deleted')`。
  - `deleted_at` 配合 `uq_users_email` 唯一索引要加 `WHERE deleted_at IS NULL` 条件。
  - `User` struct（`src/services/api/internal/data/users_repo.go`）需要同步扩展字段。
  - `auth.Service` 的 `AuthenticateUser` 需要检查 `status != 'suspended'`，suspended 用户拒绝登录。
- **具体改动范围**：
  - 新建 `src/services/api/internal/migrate/migrations/00012_users_add_email_status.sql`
  - 修改 `src/services/api/internal/data/users_repo.go`：`User` struct 增加字段，`Create`/`GetByID` 更新 SELECT/INSERT。
  - 修改 `src/services/api/internal/auth/service.go`：登录时校验 `status`。
  - 修改 `src/services/api/internal/migrate/migrate.go`：`ExpectedVersion` 递增。
- **验收**：
  - `go test -tags integration ./internal/migrate/...`：migration up/down 无报错。
  - `go test -tags integration ./internal/http/...`：auth 测试通过（现有用户 email=NULL 不影响登录）。
  - 手工：`psql` 确认 `\d users` 新字段存在且约束生效。

### R11 -- orgs 表补全（owner + status + settings）

- **关联审计**：二.2.2「组织模型过于简陋」
- **关联目标架构**：六.6.2 `ALTER TABLE orgs ADD COLUMN owner_user_id/status/settings_json/...`
- **目标**：`orgs` 增加 `owner_user_id`、`status`、`country`、`timezone`、`logo_url`、`settings_json`、`deleted_at`。
- **关键点**：
  - `owner_user_id` nullable（现有 org 没有 owner，后续手动回填或在注册时自动设置）。
  - `settings_json` 默认 `'{}'::jsonb`，为后续 BYOK 开关、MFA 强制等 org 级配置预留。
  - `status` CHECK `('active', 'suspended')`。
  - `Org` struct（`src/services/api/internal/data/orgs_repo.go`）同步扩展。
- **具体改动范围**：
  - 新建 migration `00013_orgs_add_owner_status.sql`
  - 修改 `orgs_repo.go`
- **验收**：
  - `go test -tags integration ./internal/migrate/...`
  - 手工：`\d orgs` 确认新字段。

### R12 -- runs 表补全（生命周期 + 成本 + status CHECK）

- **关联审计**：二.2.4「Run 表缺少关键生命周期字段」；P1「runs.status 不可信」
- **关联目标架构**：六.6.6 `ALTER TABLE runs ADD COLUMN completed_at/failed_at/duration_ms/total_input_tokens/...`
- **目标**：`runs` 增加 `parent_run_id`、`status_updated_at`、`completed_at`、`failed_at`、`duration_ms`、`total_input_tokens`、`total_output_tokens`、`total_cost_usd`、`model`、`skill_id`、`deleted_at`。为 `status` 加 CHECK 约束。
- **关键点**：
  - 现有 `runs.status` 全部为 `'running'`（Worker 写入后从不更新）。migration 需要先把已有的非法状态修正后再加 CHECK。
  - `migration/00011_fix_stale_running_runs.sql` 已存在相关逻辑，本次在其基础上扩展。
  - CHECK 允许的值：`('running', 'completed', 'failed', 'cancelled', 'cancelling')`。
  - `Run` struct（`runs_repo.go`）同步扩展。
  - Worker 的 `eventWriter` 在写入终态事件（`run.completed`/`run.failed`/`run.cancelled`）时，需要同步 `UPDATE runs SET status=$1, completed_at/failed_at=now(), duration_ms=... WHERE id=$2`。这是 **R30**（Phase 3）的工作，R12 只做 schema。
- **具体改动范围**：
  - 新建 migration `00014_runs_add_lifecycle_fields.sql`
  - 修改 `runs_repo.go`：`Run` struct 增加字段；新增 `UpdateRunTerminalStatus` 方法（供 Worker 调用）。
- **验收**：
  - `go test -tags integration ./internal/migrate/...`
  - `go test -tags integration ./internal/http/...`：现有 runs API 测试通过。
  - 手工：对已有 `running` 状态的 run 不受影响；手动 INSERT 非法 status 被拒绝。

### R13 -- run_events 序号分配改为 PostgreSQL sequence

- **关联审计**：二.2.5「事件表有三个定时炸弹 -- 第一个：行级锁热点」；P0
- **关联目标架构**：六.6.7 `CREATE SEQUENCE run_events_seq_global`
- **目标**：消除 `UPDATE runs SET next_event_seq = next_event_seq + 1` 的行级锁争用，改用 PostgreSQL 全局 sequence。
- **关键点**：
  - 创建 `run_events_seq_global` sequence。
  - 修改 `run_events.seq` 的 DEFAULT 为 `nextval('run_events_seq_global')`。
  - `runs.next_event_seq` 字段不再需要，但不急着 DROP（先保留，Phase 完成后清理）。
  - **API 侧**：`RunEventRepository.allocateSeq()`（`runs_repo.go`）改为 `SELECT nextval('run_events_seq_global')`。
  - **Worker 侧**：`data.RunEventsRepository.AppendEvent()`（`src/services/worker/internal/data/`）同样改为使用 sequence。
  - sequence 是全局递增的，不再保证同一 run 内的 seq 连续。SSE 的 `after_seq` 语义不变（大于某个 seq 的所有事件），但前端不应假设 seq 连续。
  - 需要检查前端 SSE 消费逻辑是否依赖 seq 连续性（当前 `after_seq` 只做 `> $1` 比较，不依赖连续）。
- **具体改动范围**：
  - 新建 migration `00015_run_events_use_sequence.sql`：创建 sequence，设置 START 为当前 `run_events.seq` 最大值 + 1。
  - 修改 `src/services/api/internal/data/runs_repo.go`：`allocateSeq` 改为 `SELECT nextval`。
  - 修改 `src/services/worker/internal/data/`：同步修改事件写入逻辑。
- **验收**：
  - `go test -tags integration ./...`（api + worker）
  - 手工：启动 API + Worker，创建 run，SSE 事件流 seq 单调递增且不出现行锁等待。
  - 压测验证（可选）：并发创建 10 个 run，观察 `pg_stat_activity` 不出现 `wait_event_type = Lock` 在 `runs` 表上。

### R14 -- messages 增加 content_json（多模态预留）

- **关联审计**：二.2.3「消息表不支持多模态」；P1
- **关联目标架构**：六.6.5 `ALTER TABLE messages ADD COLUMN content_json JSONB`
- **目标**：`messages` 增加 `content_json`、`metadata_json`、`deleted_at`、`token_count`。现有 `content TEXT` 保留不动（向后兼容）。
- **关键点**：
  - `content_json` nullable。当 `content_json` 为 NULL 时，读取 `content`（纯文本）；当 `content_json` 非 NULL 时，优先使用 `content_json`。
  - `content_json` 格式参考 Anthropic Messages API（目标架构六.6.5 已定义）。
  - 本步只做 schema + repository 层适配。Worker 写入 assistant 消息时仍写 `content TEXT`（后续 R31 改为写 `content_json`）。
  - `Message` struct（`messages_repo.go`）增加 `ContentJSON`、`MetadataJSON`、`DeletedAt`、`TokenCount` 字段。
  - `ListByThread` 的 WHERE 条件增加 `AND deleted_at IS NULL`。
- **具体改动范围**：
  - 新建 migration `00016_messages_add_content_json.sql`
  - 修改 `src/services/api/internal/data/messages_repo.go`
  - 修改 `src/services/worker/internal/data/`（如果 Worker 有独立的 messages repo）
- **验收**：
  - `go test -tags integration ./...`
  - 手工：现有消息（content TEXT）正常显示；新消息写入 content_json 后 API 可正确返回。

### R15 -- threads / runs / messages 统一软删除

- **关联审计**：二.2.7「所有删除都是硬删除」；P1
- **关联目标架构**：六.6.4 `threads ADD COLUMN deleted_at`；六.6.6 `runs ADD COLUMN deleted_at`
- **目标**：`threads` 增加 `deleted_at`。（`messages.deleted_at` 在 R14 已加；`runs.deleted_at` 在 R12 已加。）所有 SELECT 查询加 `WHERE deleted_at IS NULL`。
- **关键点**：
  - `threads.deleted_at` nullable，默认 NULL。
  - `threads` 同时增加 `project_id`（nullable，为 Phase 5 teams/projects 预留，此步不建外键）。
  - 删除操作改为 `UPDATE ... SET deleted_at = now()`，不再物理删除。
  - 审计日志记录 soft delete 操作。
  - `threads_repo.go` 所有 SELECT 追加 `AND deleted_at IS NULL`。
- **具体改动范围**：
  - 新建 migration `00017_threads_add_soft_delete.sql`
  - 修改 `threads_repo.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：`DELETE` API（如果有）改为 soft delete；列表接口不返回已删除项。

### R16 -- audit_logs 补全（IP + User-Agent + 变更状态）

- **关联审计**：五.5.2「审计日志缺少关键信息」；P1
- **关联目标架构**：六.6.19 `ALTER TABLE audit_logs ADD COLUMN ip_address/user_agent/api_key_id/before_state_json/after_state_json`
- **目标**：`audit_logs` 增加 `ip_address INET`、`user_agent TEXT`、`api_key_id UUID`、`before_state_json JSONB`、`after_state_json JSONB`。
- **关键点**：
  - 全部 nullable（现有审计记录不受影响）。
  - `audit.Writer` 的写入方法需要扩展入参，从 HTTP request 中提取 IP + User-Agent。
  - 中间件 `middleware.go` 在 context 中注入 `RemoteAddr` + `User-Agent`，`audit.Writer` 从 context 读取。
  - `before_state_json`/`after_state_json` 在有状态变更的操作时由 handler 主动填写（不强制所有操作都填）。
- **具体改动范围**：
  - 新建 migration `00018_audit_logs_add_ip_ua_state.sql`
  - 修改 `src/services/api/internal/audit/`：扩展写入方法。
  - 修改 `src/services/api/internal/http/middleware.go`：context 注入。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：登录后查看 `audit_logs` 表，确认 `ip_address` 和 `user_agent` 已填充。

---

## 4. Phase 2 -- 基础设施引入

目标：引入 Redis 和对象存储（MinIO），为 Gateway、实时推送、分布式锁、缓存等后续改造提供基础设施。

### R20 -- compose.yaml 引入 Redis

- **关联审计**：P0「没有 Redis」
- **关联目标架构**：七「Redis 规范」；十三「compose.yaml 目标」
- **目标**：`compose.yaml` 新增 Redis service，Go 服务引入 `go-redis` 依赖并验证连通性。
- **关键点**：
  - Redis 7.x-alpine，端口 6379，持久化策略 appendonly（开发环境用）。
  - 新增 `ARKLOOP_REDIS_URL` 环境变量（格式 `redis://:password@host:port/db`）。
  - API 和 Worker 的 `go.mod` 各加 `github.com/redis/go-redis/v9`。
  - 先只做连通性验证（ping），不在业务逻辑中使用。
  - 健康检查：`redis-cli ping`。
- **具体改动范围**：
  - 修改 `compose.yaml`：新增 `redis` service。
  - 新建 `src/services/api/internal/data/redis.go`：连接池初始化 + Ping。
  - 新建 `src/services/worker/internal/data/redis.go`：同上。
  - `.env.example` 新增 `ARKLOOP_REDIS_URL`。
- **验收**：
  - `docker compose up -d` 后 `docker compose exec redis redis-cli ping` 返回 PONG。
  - `go test -tags integration ./...`（包含 Redis ping 测试）。

### R21 -- compose.yaml 引入 MinIO（对象存储）

- **关联审计**：P2「messages 大内容无限存 DB」
- **关联目标架构**：八「对象存储」；十三 `compose.yaml` 新增 `minio`
- **目标**：`compose.yaml` 新增 MinIO service，验证 S3 兼容接口可用。
- **关键点**：
  - MinIO 最新稳定版，console 端口 9001，API 端口 9000。
  - 新增 `ARKLOOP_S3_ENDPOINT`、`ARKLOOP_S3_ACCESS_KEY`、`ARKLOOP_S3_SECRET_KEY` 环境变量。
  - 本步只验证连通性和 bucket 创建，不在业务中使用。
  - Go 侧引入 `github.com/minio/minio-go/v7` 或 `github.com/aws/aws-sdk-go-v2/service/s3`（选 S3 兼容接口，生产可无缝切换 AWS/GCS/OSS）。
- **具体改动范围**：
  - 修改 `compose.yaml`
  - `.env.example` 新增 S3 相关变量
  - 新建 `src/services/api/internal/data/s3.go`（或 `objectstore.go`）：初始化 + 创建 bucket + 上传/下载测试
- **验收**：
  - `docker compose up -d` 后访问 MinIO console `http://localhost:9001` 可登录。
  - `go test -tags integration ./...`（包含 S3 连通性测试）。

---

## 5. Phase 3 -- Worker 执行路径重构

目标：解决 Worker 侧的结构性问题 -- MCP 远端化、Skills 入库、凭证从环境变量迁移到数据库、事件写入路径优化、runs.status 同步更新。

### R30 -- Worker 写入终态时同步更新 runs 状态

- **关联审计**：二.2.4「runs.status 不可信」；P1
- **依赖**：R12（runs 表已有新字段）
- **目标**：Worker 在写入 `run.completed` / `run.failed` / `run.cancelled` 事件时，同步执行 `UPDATE runs SET status=..., completed_at/failed_at=now(), duration_ms=..., total_input_tokens=..., total_output_tokens=..., total_cost_usd=...`。
- **关键点**：
  - 更新在同一个事务内完成（`eventWriter.Append` 写事件和更新 runs 在同一个 tx 中）。
  - `duration_ms` = `now() - runs.created_at` 的毫秒数。
  - `total_input_tokens` / `total_output_tokens` / `total_cost_usd` 从 LLM 响应的 usage 字段汇总（当前 `LlmStreamRunCompleted.Usage` 已有）。需要在 `eventWriter` 或 `AgentLoop` 中累加多轮的 usage。
  - `model` / `skill_id` 在 `run.started` 时从 `inputJSON` 写入。
  - 更新完成后，`runs.status` 成为可信字段，API 查询不再需要扫描 `run_events` 推导状态。
- **具体改动范围**：
  - 修改 `src/services/worker/internal/runengine/v1.go`：`eventWriter` 在写入终态事件时调用 `RunsRepository.UpdateTerminalStatus`。
  - 修改 `src/services/worker/internal/data/`：新增 `UpdateTerminalStatus` 方法（或在 API 的 data 包中如果共享）。
  - 修改 `src/services/worker/internal/agent/`：累加多轮 usage。
- **验收**：
  - `go test -tags integration ./...`（worker）
  - 手工：创建 run 后等待完成，查询 `SELECT status, completed_at, duration_ms, total_input_tokens FROM runs WHERE id = $1` 确认字段已更新。

### R31 -- 消除 SSE 双重延迟

- **关联审计**：四「实时推送链路是假的」；P1「SSE 双重延迟」
- **关联目标架构**：五「实时推送」；五.5.1 `pg_notify('run_events:{run_id}', seq)`
- **目标**：消除 Worker 200ms 批提交 + API 250ms 轮询的双重延迟。
- **关键点**：
  - **Worker 侧**：`message.delta` 等流式事件不再攒批，每个事件立即提交（或至少每个事件立即 `pg_notify`，批提交间隔缩短到 50ms 以内）。
  - **API 侧**：SSE handler 改为 `LISTEN run_events:{run_id}` 驱动，不再 250ms 轮询。收到 NOTIFY 后立即查库推送。
  - 性能权衡：delta 事件每个都单独 commit 会增加 PG 负载。折中方案是保留批提交但将间隔从 200ms 降到 50ms，同时每次 commit 后立即 `pg_notify`。API 侧用 LISTEN 替代轮询。
  - 多副本 API：Phase 2 引入 Redis 后，`pg_notify` 只能到达一个 API 实例（LISTEN 的那个）。需要 Redis Pub/Sub 做跨实例广播。本步先做单实例优化（`pg_notify` + LISTEN），多实例广播在 R20 Redis 引入后补充。
- **具体改动范围**：
  - 修改 `src/services/worker/internal/runengine/v1.go`：`eventCommitMaxInterval` 从 200ms 降到 50ms；commit 后执行 `pg_notify`。
  - 修改 `src/services/api/internal/http/v1_runs.go`：SSE handler 从轮询改为 LISTEN 驱动。
- **验收**：
  - 手工：浏览器中观察流式输出，token 到达间隔明显缩短（从批量到达变为接近逐 token 到达）。
  - `go test -tags integration ./...`

### R32 -- secrets 统一管理表

- **关联审计**：五.5.1「没有 secrets 表」；P1
- **关联目标架构**：六.6.8 `CREATE TABLE secrets`；九「凭证加密」
- **目标**：创建 `secrets` 表，实现应用层加密（AES-256-GCM），为后续 MCP auth token、LLM API Key 的入库存储提供统一基础。
- **关键点**：
  - 主密钥从 `ARKLOOP_ENCRYPTION_KEY` 环境变量读取（32 字节 hex）。
  - `secrets` 表字段：`id`、`org_id`、`name`、`encrypted_value`、`key_version`、`created_at`、`updated_at`、`rotated_at`。
  - `encrypted_value` 格式：`base64(nonce + ciphertext + tag)`。
  - `key_version` 支持密钥轮换：解密时根据 `key_version` 选择对应密钥。
  - 新建 `src/services/api/internal/crypto/` 包：`Encrypt(plaintext, keyVersion) -> ciphertext`、`Decrypt(ciphertext, keyVersion) -> plaintext`。
  - 新建 `src/services/api/internal/data/secrets_repo.go`：CRUD + 按 org_id + name 唯一查找。
- **具体改动范围**：
  - 新建 migration `00019_create_secrets.sql`
  - 新建 `src/services/api/internal/crypto/envelope.go`
  - 新建 `src/services/api/internal/data/secrets_repo.go`
- **验证**：
  - `go test ./internal/crypto/...`：加密/解密 round-trip；无密钥时解密失败；新旧版本兼容。
  - `go test -tags integration ./internal/data/...`：secrets CRUD。

### R33 -- LLM 凭证入库（从环境变量迁移到数据库）

- **关联审计**：三.3.3「LLM 凭证锁死在环境变量里」；P1
- **关联目标架构**：六.6.10 `CREATE TABLE llm_credentials` + `llm_routes`
- **依赖**：R32（secrets 表 + 加密能力）
- **目标**：创建 `llm_credentials` 和 `llm_routes` 表。Worker 的 `routing/config.go` 支持从数据库加载路由配置（同时保留环境变量作为 fallback）。
- **关键点**：
  - `llm_credentials` 的 `secret_id` 关联 `secrets` 表，API Key 加密存储。
  - 环境变量 `ARKLOOP_PROVIDER_ROUTING_JSON` 保留为 fallback（当数据库中无路由配置时使用）。
  - Worker 启动时优先从数据库加载路由；数据库无配置则回退到环境变量。
  - API 侧新增管理端点（P44 Console 凭证管理的后端部分）：
    - `POST /v1/llm-credentials`
    - `GET /v1/llm-credentials`
    - `DELETE /v1/llm-credentials/{id}`
  - 安全：API 响应永远不返回明文 API Key（只返回 key_prefix + 掩码）。
- **具体改动范围**：
  - 新建 migration `00020_create_llm_credentials_routes.sql`
  - 新建 `src/services/api/internal/data/llm_credentials_repo.go`
  - 新建 `src/services/api/internal/data/llm_routes_repo.go`
  - 修改 `src/services/worker/internal/routing/config.go`：支持从 DB 加载。
  - 新建 `src/services/api/internal/http/v1_llm_credentials.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：通过 API 创建 LLM 凭证 -> Worker 使用该凭证执行 run -> 成功。

### R34 -- MCP 配置入库 + Remote HTTP/SSE 传输

- **关联审计**：三.3.1「MCP 是单机 IDE 的设计，不是 SaaS 的设计」；P0
- **关联目标架构**：六.6.9 `CREATE TABLE mcp_configs`；十「MCP 架构」
- **依赖**：R32（secrets 表 + 加密能力）
- **目标**：创建 `mcp_configs` 表，MCP 配置从数据库加载（保留文件系统 fallback），支持 Remote HTTP/SSE 传输。
- **关键点**：
  - `mcp_configs` 支持三种 transport：`http_sse`、`streamable_http`、`stdio`。
  - `auth_secret_id` 关联 `secrets` 表（Bearer token 加密存储）。
  - `mcp/config.go` 的 `LoadConfigFromEnv()` 保留为 fallback；新增 `LoadConfigFromDB(ctx, pool, orgID)` 从数据库加载。
  - Worker 执行 Run 时按 org_id 加载该 org 的 MCP 配置。
  - `mcp/pool.go` 的 key 从 `server_id` 改为 `(org_id, server_id)`，避免跨租户共享。
  - 新增 Remote HTTP 传输的 MCP client 实现（当前只有 stdio）。
  - API 侧新增管理端点：
    - `POST /v1/mcp-configs`
    - `GET /v1/mcp-configs`
    - `PATCH /v1/mcp-configs/{id}`
    - `DELETE /v1/mcp-configs/{id}`
- **具体改动范围**：
  - 新建 migration `00021_create_mcp_configs.sql`
  - 新建 `src/services/api/internal/data/mcp_configs_repo.go`
  - 新建 `src/services/api/internal/http/v1_mcp_configs.go`
  - 修改 `src/services/worker/internal/mcp/config.go`：`LoadConfigFromDB`
  - 新建 `src/services/worker/internal/mcp/http_client.go`：Remote HTTP/SSE 传输
  - 修改 `src/services/worker/internal/mcp/pool.go`：key 改为 `(org_id, server_id)`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：通过 API 创建 MCP 配置 -> Worker 使用远程 MCP Server 执行工具调用。

### R34.5 -- LLM 路由配置 per-run 动态加载

- **背景**：R33 实现了 Worker 启动时从 DB 一次性加载 LLM 路由配置，但 R34 的 MCP 工具已实现 per-run 动态加载。两者行为不一致：凭证/路由变更后必须重启 Worker 才能生效，而 MCP 配置变更无需重启。
- **目标**：将 LLM 路由配置的加载时机从「Worker 启动」改为「per-run 执行」，与 MCP 工具的动态加载模式对齐。
- **关键点**：
  - `EngineV1` 不再在构造时持有静态 `Router`，改为在 `Execute` 时按需从 DB 加载路由配置并构建 `ProviderRouter`。
  - 路由解析结果不跨 run 缓存（每个 run 独立查询），保证配置变更实时生效，无需重启 Worker。
  - `ComposeNativeEngine` 中的 `loadRoutingConfig` 仍保留，用于初始化 stub/env-only 路由配置作为 fallback。
  - `EngineV1Deps` 新增 `DBPool *pgxpool.Pool` 字段，供 `Execute` 内部按 run 加载路由。
  - 回退逻辑：DB 无可用路由时回退到 `EngineV1` 初始化时的静态路由配置（即当前 env var 加载结果）。
- **具体改动范围**：
  - 修改 `src/services/worker/internal/runengine/v1.go`：`Execute` 方法开始时调用 `routing.LoadRoutingConfigFromDB`，构建 per-run `ProviderRouter`；DB 失败时回退到 `e.router`（静态 fallback）。
  - 修改 `src/services/worker/internal/app/composition.go`：`EngineV1Deps` 注入 `DBPool`。
- **验收**：
  - `go test -tags integration ./...`（worker）
  - 手工：Worker 运行中通过 API 修改 LLM 凭证 -> 无需重启，下一个 run 立即使用新凭证。

### R35 -- Skills 入库

- **关联审计**：三.3.4「Skills 绑定在文件系统」；P1
- **关联目标架构**：六.6.11.3 `CREATE TABLE skills`
- **目标**：创建 `skills` 表，Skills 从数据库加载（保留文件系统 fallback）。
- **关键点**：
  - `skills` 表字段：`id`、`org_id`（NULL 表示全局 skill）、`skill_key`、`version`、`display_name`、`description`、`prompt_md`、`tool_allowlist`、`budgets_json`、`is_active`、`created_at`。
  - 唯一约束 `(org_id, skill_key, version)`。
  - Worker 的 `skills/loader.go` 保留文件系统加载作为 fallback（内置 skill）；新增从 DB 加载的路径。
  - DB 中的 org 级 skill 覆盖文件系统中的同名 skill。
  - API 侧新增管理端点（面向后续 Console）：
    - `POST /v1/skills`
    - `GET /v1/skills`
    - `PATCH /v1/skills/{id}`
- **具体改动范围**：
  - 新建 migration `00022_create_skills.sql`
  - 新建 `src/services/api/internal/data/skills_repo.go`
  - 新建 `src/services/api/internal/http/v1_skills.go`
  - 修改 `src/services/worker/internal/skills/loader.go`：`LoadFromDB` + fallback 逻辑
- **验收**：
  - `go test -tags integration ./...`
  - 手工：通过 API 创建 skill -> 使用该 skill_id 创建 run -> skill 约束生效。

### R36 -- Worker 注册与心跳 + 任务路由标签

- **关联审计**：三.3.2「Worker 调度是盲目的」；P0
- **关联目标架构**：六.6.17 `CREATE TABLE worker_registrations`；六.6.18 `jobs ADD COLUMN worker_tags`
- **依赖**：R20（Redis）
- **目标**：Worker 启动时注册到 Redis + 数据库，上报能力标签。任务调度按标签匹配。
- **关键点**：
  - Worker 启动时生成 `worker_id`（UUID），写入 Redis Hash `arkloop:worker:{worker_id}`（TTL 30s，每 10s 心跳续期）并同步写入 `worker_registrations` 表。
  - `worker_registrations` 字段：`id`、`worker_id`、`hostname`、`version`、`status`（active/draining/dead）、`capabilities JSONB`、`current_load`、`max_concurrency`、`heartbeat_at`、`registered_at`。
  - `jobs` 增加 `worker_tags TEXT[]`。Worker Lease 时 SQL 加 `worker_tags <@ $capabilities` 过滤。
  - Worker 停止时标记 `status = 'draining'` -> `'dead'`，Redis key DEL。
- **具体改动范围**：
  - 新建 migration `00023_create_worker_registrations.sql`
  - 新建 migration `00024_jobs_add_worker_tags.sql`
  - 新建 `src/services/worker/internal/registration/` 包
  - 修改 `src/services/worker/internal/consumer/loop.go`：Lease SQL 加 tag 过滤
  - 修改 `src/services/worker/cmd/worker/main.go`：启动/停止生命周期注册
- **验收**：
  - `go test -tags integration ./...`
  - 手工：启动 Worker，查看 `worker_registrations` 表有记录；`redis-cli hgetall arkloop:worker:{id}` 有数据；停止 Worker 后 status 变为 dead。

---

## 6. Phase 4 -- Gateway + 安全基础

目标：引入独立 Gateway 层，实现限流、IP 过滤、API Key 验证，建立外部访问的安全屏障。

### R40 -- Gateway 独立服务骨架

- **关联审计**：一「流量入口：没有 Gateway 层」；P0
- **关联目标架构**：二「Gateway 层」
- **依赖**：R20（Redis）
- **目标**：新建 `src/services/gateway/` 独立 Go 服务，作为 API 的反向代理，承担 token 验证、限流、IP 过滤。
- **关键点**：
  - 技术选型：Go `net/http/httputil.ReverseProxy`，复用现有技术栈。
  - Gateway 是 stateless 的，不持有数据库连接（只访问 Redis）。
  - 启动时从环境变量读取 upstream API 地址。
  - 第一版只做透传（验证 Gateway -> API 链路可用），限流和 IP 过滤在后续 R41/R42 实现。
  - `compose.yaml` 新增 `gateway` service。
- **具体改动范围**：
  - 新建 `src/services/gateway/` 目录结构（`cmd/gateway/main.go`、`internal/proxy/`）
  - 修改 `compose.yaml`：新增 gateway service
- **验收**：
  - `docker compose up -d` 后通过 Gateway 端口访问 API，所有现有接口正常工作。
  - `go test ./...`（gateway）

### R41 -- Gateway 限流（Redis token bucket）

- **关联审计**：一「没有任何限流机制」
- **依赖**：R40 + R20
- **目标**：Gateway 按 org_id 实现 Redis token bucket 限流。
- **关键点**：
  - Redis key：`arkloop:ratelimit:{org_id}:{window}`（窗口可按秒/分/时）。
  - 限流策略通过环境变量配置（后续迁移到数据库）：默认每分钟 60 请求/org。
  - 超限返回 HTTP 429 + `Retry-After` header。
  - SSE 长连接不计入普通请求限流（建议按连接数限制）。
- **具体改动范围**：
  - 新建 `src/services/gateway/internal/ratelimit/`
  - 修改 proxy 中间件链
- **验收**：
  - `go test ./...`（包含限流逻辑单测）
  - 手工：短时间内大量请求，确认 429 返回且 `Retry-After` 合理。

### R42 -- Gateway IP 过滤

- **关联审计**：一「IP 过滤放在 API 服务里会绕过非标准路径」
- **关联目标架构**：六.6.13 `CREATE TABLE ip_rules`
- **依赖**：R40 + R20
- **目标**：Gateway 从 Redis 缓存加载 org 的 IP allowlist/blocklist，在请求转发前检查。
- **关键点**：
  - `ip_rules` 表字段：`id`、`org_id`、`type`（allowlist/blocklist）、`cidr CIDR`、`note`、`created_at`。
  - Redis 缓存 key：`arkloop:ip_rules:{org_id}`，TTL 5min。
  - 数据库变更时通过 API 端点触发缓存失效。
  - 默认无规则 = 全部放行。
- **具体改动范围**：
  - 新建 migration `00025_create_ip_rules.sql`
  - 新建 `src/services/gateway/internal/ipfilter/`
  - 新建 `src/services/api/internal/data/ip_rules_repo.go`
  - 新建 `src/services/api/internal/http/v1_ip_rules.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：添加 blocklist 规则后，对应 IP 请求被拒绝。

### R43 -- API Key 管理

- **关联审计**：六「API Key 管理不存在」；P2
- **关联目标架构**：六.6.12 `CREATE TABLE api_keys`
- **依赖**：R40（Gateway 做 API Key 验证）
- **目标**：实现 API Key 的创建、查询、吊销、鉴权。
- **关键点**：
  - 创建时生成随机 key，返回明文（仅此一次）；存储 SHA-256 hash。
  - `key_prefix`：前 8 位，用于展示识别。
  - `scopes TEXT[]`：权限范围（初版可先全权限）。
  - Gateway 鉴权路径：`Authorization: Bearer ak-xxx` -> SHA-256 hash -> 查 `api_keys` 表（Redis 缓存 5min）。
  - 区分 JWT token 和 API Key：前缀 `ak-` 表示 API Key。
  - 审计：API Key 的创建、使用、吊销全部写审计日志。
- **具体改动范围**：
  - 新建 migration `00026_create_api_keys.sql`
  - 新建 `src/services/api/internal/data/api_keys_repo.go`
  - 新建 `src/services/api/internal/http/v1_api_keys.go`
  - 修改 Gateway 鉴权中间件：支持 API Key 路径
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 API Key -> 用 API Key 访问接口 -> 吊销后被拒绝。

### R44 -- 并发 Run 限制

- **关联审计**：五.5.3「没有并发限制」；P2
- **关联目标架构**：二.2.1「并发 Run 限制：检查 org 当前活跃 run 数」
- **依赖**：R20（Redis）
- **目标**：限制单个 org 同时运行的 run 数量。
- **关键点**：
  - Redis counter：`arkloop:org:active_runs:{org_id}`。创建 run 时 INCR，run 终态时 DECR。
  - 默认限制：每 org 10 个并发 run（通过环境变量或 org settings 配置）。
  - 超限时 API 返回 HTTP 429 + 明确错误码 `runs.concurrent_limit_exceeded`。
  - Worker 终态写入时在同一事务中 DECR Redis counter（需要处理 Worker 崩溃导致的计数器泄漏，定期从数据库重新同步）。
- **具体改动范围**：
  - 修改 `src/services/api/internal/http/v1_runs.go`：创建 run 时检查 + INCR
  - 修改 `src/services/worker/internal/runengine/v1.go`：终态时 DECR
  - 新建 `src/services/api/internal/data/run_limiter.go`：封装 Redis 计数逻辑
- **验证**：
  - `go test -tags integration ./...`
  - 手工：快速创建超过限制数量的 run，确认被拒绝。

---

## 6.5 Phase 4.5 -- Worker 执行 Pipeline 重构

目标：将 `EngineV1.Execute` 的过程式代码重构为 Pipeline/Middleware 架构。不新增功能，保持行为完全不变，但为后续 Phase 5-7 的每个新能力（Agent Config 继承链、Memory、Content Filter、Usage Tracking）提供可插拔的扩展点。

**为什么在 Phase 5 之前做：**

当前 `runengine/v1.go` 的 `Execute` 是一个约 300 行的过程式函数。Phase 1-4 的每次新增能力（MCP 动态加载、per-run 路由、Skills DB 加载）都是往这个函数里追加代码。如果不在此时引入 Pipeline 抽象，Phase 5 的 Agent Config 继承链、Phase 6 的 Content Filter 和 Usage Tracking、以及未来的 Memory System 都将继续膨胀这个函数，最终导致不可维护。

Pipeline 机制的核心思想：把 Execute 方法拆成一串独立的处理环节（Middleware），每个环节只做一件事，环节之间通过统一接口串联。新能力只需编写新的 Middleware 并注册到 Pipeline，不修改现有代码。

### R45 -- Pipeline 框架定义（RunContext + RunMiddleware + RunHandler）

- **关联**：无直接审计对应。解决 `runengine/v1.go` Execute 方法的可扩展性瓶颈。
- **目标**：定义 `RunContext`、`RunMiddleware`、`RunHandler` 接口和 Pipeline 组装器。
- **关键点**：
  - `RunContext` 是贯穿整条 Pipeline 的上下文结构体，携带 Run 执行所需的全部可变状态。每个 Middleware 可以读写 RunContext 的字段来影响后续环节。
  - `RunMiddleware` 签名：`func(ctx context.Context, rc *RunContext, next RunHandler) error`。做完自己的工作后调用 `next` 传递控制权。也可以在 `next` 返回后执行清理逻辑（如 Memory 写回）。不调用 `next` 即短路（如取消检查命中时）。
  - `RunHandler` 签名：`func(ctx context.Context, rc *RunContext) error`。Pipeline 的终端处理器。
  - `Build(middlewares []RunMiddleware, terminal RunHandler) RunHandler`：组装函数，将 Middleware 列表和终端 Handler 构建为一个可调用的 RunHandler。组装顺序即执行顺序。
  - `RunContext` 字段从现有 Execute 方法中提取，包括但不限于：
    - `Run data.Run`、`Pool *pgxpool.Pool`
    - `InputJSON map[string]any`
    - `Messages []llm.Message`（线程历史消息，Middleware 可追加/修改）
    - `SystemPrompt string`（Skill 或 Agent Config 设置）
    - `ToolSpecs []llm.ToolSpec`、`ToolExecutors map[string]tools.Executor`、`AllowlistSet map[string]struct{}`
    - `Router *routing.ProviderRouter`、`Gateway llm.Gateway`
    - `MaxIterations int`、`MaxOutputTokens *int`、`ToolTimeoutMs *int`、`ToolBudget map[string]any`
    - `SkillDefinition *skills.Definition`
    - `Emitter events.Emitter`、`TraceID string`
  - Pipeline 框架本身不依赖业务逻辑（不引入 pgxpool、redis），是纯组合工具。
- **具体改动范围**：
  - 新建 `src/services/worker/internal/pipeline/context.go`：`RunContext` 定义。
  - 新建 `src/services/worker/internal/pipeline/middleware.go`：`RunMiddleware`、`RunHandler` 类型定义 + `Build` 组装函数。
  - 新建 `src/services/worker/internal/pipeline/pipeline_test.go`：验证 Middleware 串联顺序、RunContext 字段传递、短路行为、`next` 后清理逻辑。
- **验收**：
  - `go test ./internal/pipeline/...`：组装器单测全绿。
  - 框架代码无外部依赖（不引入 pgxpool、redis 等）。

### R46 -- Execute 重构为 Pipeline 调度

- **依赖**：R45
- **目标**：将 `EngineV1.Execute` 中的各步骤拆为独立 Middleware，用 Pipeline 组装替代过程式调用。外部行为（事件写入、SSE 推送、runs.status 更新、并发计数 DECR）完全不变。
- **关键点**：
  - 从现有 Execute 中提取以下 Middleware（按执行顺序）：
    1. **CancelGuardMiddleware**：检查 run 是否已取消/已终态，设置 LISTEN/NOTIFY 取消信号桥接到 context。对应 Execute 开头的 `readLatestEventType` + LISTEN 逻辑。
    2. **InputLoaderMiddleware**：加载 `inputJSON` + 线程历史消息到 `RunContext.Messages`。对应 `loadRunInputs`。
    3. **MCPDiscoveryMiddleware**：按 org 从 DB 加载 MCP 工具，合并到 `RunContext.ToolSpecs` 和 `RunContext.ToolExecutors`。对应 `mcp.DiscoverFromDB` 段落。
    4. **SkillResolutionMiddleware**：加载 org skills 并解析 skill_id，设置 `RunContext.SystemPrompt`、`RunContext.MaxIterations`、`RunContext.ToolBudget` 等。对应 `skills.ResolveSkill` 段落。
    5. **RoutingMiddleware**：per-run 从 DB 加载路由配置，执行路由决策，构建 LLM Gateway 写入 `RunContext.Gateway`。对应 `routing.LoadRoutingConfigFromDB` + `router.Decide` + `gatewayFromCredential` 段落。
    6. **ToolBuildMiddleware**：根据最终的 allowlist 构建 `DispatchingExecutor` 和过滤后的 `ToolSpecs`。对应 `buildDispatchExecutor` + `filterToolSpecs`。
  - 终端 Handler 是 **AgentLoopHandler**：构建 `agent.RunContext` + `llm.Request`，调用 `agent.Loop.Run`，通过 `eventWriter` 写入事件和 assistant message。即现有 Execute 的后半段。
  - **eventWriter 不拆**：事件批提交和终态写入逻辑与 Agent Loop 深度耦合（cancel 检测、行锁、usage 累加），保留在 AgentLoopHandler 内部。
  - `EngineV1.Execute` 方法体缩减为：初始化 `RunContext` → 组装 Pipeline → 调用 Pipeline。
  - 每个 Middleware 放在 `src/services/worker/internal/pipeline/` 下独立文件。
  - `EngineV1` 在构造时（`NewEngineV1`）预组装 Middleware 列表，Execute 时按 run 参数实例化 RunContext 并执行。
  - **铁律：重构后所有现有 integration test 必须不修改地通过。**
- **具体改动范围**：
  - 新建 `src/services/worker/internal/pipeline/mw_cancel_guard.go`
  - 新建 `src/services/worker/internal/pipeline/mw_input_loader.go`
  - 新建 `src/services/worker/internal/pipeline/mw_mcp_discovery.go`
  - 新建 `src/services/worker/internal/pipeline/mw_skill_resolution.go`
  - 新建 `src/services/worker/internal/pipeline/mw_routing.go`
  - 新建 `src/services/worker/internal/pipeline/mw_tool_build.go`
  - 新建 `src/services/worker/internal/pipeline/handler_agent_loop.go`
  - 修改 `src/services/worker/internal/runengine/v1.go`：`Execute` 方法改为组装 Pipeline 并调用，原有私有方法（`loadRunInputs`、`gatewayFromCredential` 等）迁移到对应 Middleware。
  - 修改 `src/services/worker/internal/app/composition.go`：适配 Middleware 列表注入。
- **验收**：
  - `go test -tags integration ./...`（worker 全量）：全绿，无任何测试修改。
  - `go test ./internal/pipeline/...`：每个 Middleware 的单测覆盖正常路径和错误路径。
  - 手工：创建 run → SSE 流式输出 → run 完成 → runs.status 更新 → 并发计数 DECR，行为与重构前完全一致。
  - 代码审查：`v1.go` 的 `Execute` 方法不超过 30 行。

---

## 7. Phase 5 -- 组织模型与权限

目标：完善组织模型，建立邀请机制、RBAC 权限体系、teams/projects 层级结构。

### R50 -- org 邀请机制

- **关联审计**：二.2.2「没有邀请机制」；P1
- **关联目标架构**：六.6.2 `CREATE TABLE org_invitations`
- **目标**：创建 `org_invitations` 表，实现邀请链接的创建、接受、过期处理。
- **关键点**：
  - `org_invitations` 字段：`id`、`org_id`、`invited_by_user_id`、`email`、`role`、`token`（唯一随机字符串）、`expires_at`、`accepted_at`、`created_at`。
  - 邀请 token 有效期默认 7 天。
  - 接受邀请时自动创建 `org_memberships` 记录。
  - 审计：邀请创建、接受、过期全部写审计。
- **具体改动范围**：
  - 新建 migration `00027_create_org_invitations.sql`
  - 新建 `src/services/api/internal/data/org_invitations_repo.go`
  - 新建 `src/services/api/internal/http/v1_org_invitations.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建邀请 -> 用 token 接受 -> 确认成为 org member。

### R51 -- RBAC 权限体系 v1

- **关联审计**：六「RBAC 只有一个 role TEXT 字段」；P2
- **关联目标架构**：六.6.3 `CREATE TABLE rbac_roles`
- **依赖**：无（可独立于 R50）
- **目标**：创建 `rbac_roles` 表 + 内置角色，建立权限点体系。
- **关键点**：
  - 内置角色（`is_system = true`）：`platform_admin`、`org_admin`、`org_member`。
  - 每个角色映射一组权限点字符串数组（`permissions TEXT[]`）。
  - 权限点命名空间：`platform.*`、`org.*`、`data.*`。
  - `org_memberships` 增加 `role_id UUID REFERENCES rbac_roles(id)`。
  - 中间件层：从 JWT claims 或 membership 查询角色 -> 校验权限点。
  - 第一版先做"基于内置角色的权限点校验"，不做可编辑角色（后续迁代）。
- **具体改动范围**：
  - 新建 migration `00028_create_rbac_roles.sql`
  - 新建 `src/services/api/internal/auth/permissions.go`：权限点常量
  - 新建 `src/services/api/internal/auth/roles.go`：内置角色定义
  - 修改 `src/services/api/internal/http/middleware.go`：权限校验中间件
- **验收**：
  - `go test ./internal/auth/...`
  - `go test -tags integration ./...`：非 admin 角色无法访问管理端点。

### R52 -- Teams 与 Projects 层级

- **关联审计**：二.2.6「没有层级结构」；P2
- **关联目标架构**：六.6.4 `CREATE TABLE teams` + `projects`
- **依赖**：R15（threads 已有 `project_id` 字段预留）
- **目标**：创建 `teams`、`team_memberships`、`projects` 表，建立 `org -> team -> project -> thread` 层级。
- **关键点**：
  - `teams`：`id`、`org_id`、`name`、`created_at`。
  - `team_memberships`：`team_id`、`user_id`、`role`，唯一 `(team_id, user_id)`。
  - `projects`：`id`、`org_id`、`team_id`（nullable）、`name`、`description`、`visibility`（private/team/org）、`deleted_at`、`created_at`。
  - `threads.project_id` 加外键 `REFERENCES projects(id)`。
  - API 端点：
    - `POST/GET /v1/teams`
    - `POST/GET /v1/projects`
    - `PATCH /v1/threads/{id}`：设置 `project_id`
  - 权限：project 级 visibility 控制谁能看到 thread。
- **具体改动范围**：
  - 新建 migration `00029_create_teams_projects.sql`
  - 新建 `src/services/api/internal/data/teams_repo.go`
  - 新建 `src/services/api/internal/data/projects_repo.go`
  - 新建 `src/services/api/internal/http/v1_teams.go`
  - 新建 `src/services/api/internal/http/v1_projects.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 team -> 创建 project -> 创建 thread 指定 project_id -> 查询 thread 只返回有权限的。

---

## 8. Phase 6 -- 企业级能力

目标：补齐企业级 SaaS 必需的 Webhooks、通知、Feature Flags、Agent 行为配置等能力。

### R60 -- Webhooks

- **关联审计**：六「Webhooks 不存在」；P1
- **关联目标架构**：六.6.15 `CREATE TABLE webhook_endpoints` + `webhook_deliveries`
- **目标**：实现 Webhook 端点注册 + 事件投递 + 重试机制。
- **关键点**：
  - `webhook_endpoints`：`id`、`org_id`、`url`、`signing_secret`、`events TEXT[]`（订阅的事件类型）、`enabled`、`created_at`。
  - `webhook_deliveries`：记录每次投递的状态、响应、重试次数。
  - 投递方式：Worker 写入终态事件后，同步投递一个 `webhook.deliver` job 到队列。独立的 Webhook delivery worker 消费并 POST 到目标 URL。
  - 签名：HMAC-SHA256，`X-Arkloop-Signature` header。
  - 重试策略：指数退避，最多 5 次。
  - 支持的事件类型 v1：`run.completed`、`run.failed`、`run.cancelled`。
- **具体改动范围**：
  - 新建 migration `00030_create_webhooks.sql`
  - 新建 `src/services/api/internal/data/webhooks_repo.go`
  - 新建 `src/services/api/internal/http/v1_webhooks.go`
  - 新建 `src/services/worker/internal/webhook/` 包：delivery worker
- **验收**：
  - `go test -tags integration ./...`
  - 手工：注册 Webhook -> 创建 run -> run 完成后目标 URL 收到 POST。

### R61 -- Agent 行为配置（system prompt + 模型参数 + 工具策略）

- **关联审计**：三.3.5「整个 Agent 行为配置层不存在」
- **关联目标架构**：六.6.11 `agent_configs` + `prompt_templates`
- **依赖**：R52（projects 层级，用于配置继承链）；R46（Pipeline 架构，Agent Config 作为 Middleware 接入）
- **目标**：创建 `prompt_templates`、`agent_configs` 表，实现 `平台默认 -> org 级 -> project 级 -> thread 级` 的配置继承链。通过 Pipeline Middleware 注入 Worker 执行路径，不修改 `runengine/v1.go` 主函数。
- **关键点**：
  - `prompt_templates`：`id`、`org_id`、`name`、`content`（支持 `{{variable}}` 插值）、`variables JSONB`、`is_default`、`version`、`published_at`。
  - `agent_configs`：`id`、`org_id`、`name`、`system_prompt_template_id`、`system_prompt_override`、`model`、`temperature`、`max_output_tokens`、`top_p`、`context_window_limit`、`tool_policy`、`tool_allowlist`、`tool_denylist`、`content_filter_level`、`safety_rules_json`、`project_id`、`skill_id`、`is_default`。
  - `threads` 增加 `agent_config_id`。
  - **Middleware 接入**：新建 `AgentConfigMiddleware` 插入 R46 的 Pipeline（位于 SkillResolutionMiddleware 之前）。Middleware 按 `run -> thread -> project -> org` 逐级查找 `agent_config`，合并为最终配置写入 `RunContext`（`SystemPrompt`、`MaxIterations`、`MaxOutputTokens`、`ToolPolicy` 等字段）。SkillResolutionMiddleware 读取 RunContext 中已解析的配置作为基础，skill 的字段覆盖 agent_config 的对应字段。
  - 平台默认值保留在代码 const 中作为 fallback（`defaultAgentMaxIterations`、`threadMessageLimit` 等），仅当所有层级都无配置时使用。
  - API 端点：
    - `POST/GET/PATCH /v1/prompt-templates`
    - `POST/GET/PATCH /v1/agent-configs`
- **具体改动范围**：
  - 新建 migration `00031_create_agent_configs.sql`
  - 新建 `src/services/api/internal/data/agent_configs_repo.go`
  - 新建 `src/services/api/internal/data/prompt_templates_repo.go`
  - 新建 `src/services/api/internal/http/v1_agent_configs.go`
  - 新建 `src/services/worker/internal/pipeline/mw_agent_config.go`：`AgentConfigMiddleware`，按继承链解析配置并写入 `RunContext`。
  - 修改 `src/services/worker/internal/app/composition.go`：将 `AgentConfigMiddleware` 注册到 Pipeline。
- **验收**：
  - `go test -tags integration ./...`
  - `go test ./internal/pipeline/...`：AgentConfigMiddleware 单测覆盖继承链优先级（thread > project > org > 平台默认）。
  - 手工：创建 org 级 agent_config（temperature=0.5）-> 创建 run -> 确认使用了自定义 temperature。
  - 确认 `runengine/v1.go` 无改动。

### R62.1 -- 计划与权益模型（Plans + Entitlements Service）

- **关联审计**：六「订阅与计费不存在」
- **关联目标架构**：六.6.14 `CREATE TABLE plans/subscriptions`
- **依赖**：R46（Pipeline 架构）；R63（Feature Flags，权益检查可复用 flag 缓存模式）
- **目标**：建立权益层（Entitlements）的数据模型和统一查询服务。定义"谁被允许做什么"，不涉及用量记录和执行检查。

- **关键点**：
  - `plans`：`id`、`name`、`display_name`、`created_at`。Plan 本身只是一个标识符，不直接存储限额。
  - `plan_entitlements`：`plan_id`、`key TEXT`、`value TEXT`、`value_type TEXT CHECK ('int', 'bool', 'string')`。每个 Plan 的限额/特性用 key-value 表达。key 命名空间：`quota.*`（数量限额）、`feature.*`（功能开关）、`limit.*`（并发/速率限制）。
  - 内置 key 示例：`quota.runs_per_month`、`quota.tokens_per_month`、`limit.concurrent_runs`、`feature.byok_enabled`、`feature.mcp_remote_enabled`、`limit.team_members`。
  - `org_entitlement_overrides`：`org_id`、`key TEXT`、`value TEXT`、`value_type TEXT`、`reason TEXT`、`expires_at TIMESTAMPTZ`、`created_by_user_id`、`created_at`。后台管理员给单个 org 调整限额，支持过期时间（临时加额度）和操作原因记录。
  - 权益解析优先级：`org_entitlement_overrides`（未过期）> `plan_entitlements` > 平台硬编码默认值。
  - `subscriptions`：`id`、`org_id`、`plan_id`、`status`、`current_period_start`、`current_period_end`、`cancelled_at`、`created_at`。关联 org 与 plan。
  - Go 侧提供 `EntitlementService.Resolve(ctx, orgID, key) -> EntitlementValue`，统一查询接口。结果缓存到 Redis `arkloop:entitlement:{org_id}:{key}` TTL 5min。
  - 账单层 schema 预留：`usage_records` 按 `(org_id, recorded_at)` 索引支撑月度汇总查询。实际收费逻辑（Stripe 集成、Pay-as-you-go 超额计费）留给后续独立 R 号。

- **具体改动范围**：
  - 新建 migration `00032_create_plans_and_entitlements.sql`：`plans`、`plan_entitlements`、`subscriptions`、`org_entitlement_overrides` 四张表。
  - 新建 `src/services/api/internal/data/plans_repo.go`
  - 新建 `src/services/api/internal/data/entitlements_repo.go`：`plan_entitlements` + `org_entitlement_overrides` CRUD。
  - 新建 `src/services/api/internal/data/subscriptions_repo.go`
  - 新建 `src/services/api/internal/entitlement/service.go`：`EntitlementService.Resolve`，合并 plan + override + 默认值。
  - 新建 `src/services/api/internal/http/v1_plans.go`：Plan 管理端点（`POST/GET /v1/plans`）。
  - 新建 `src/services/api/internal/http/v1_subscriptions.go`：订阅管理端点（`POST/GET /v1/subscriptions`）。
  - 新建 `src/services/api/internal/http/v1_entitlements.go`：后台管理 override 的端点（`POST/GET/DELETE /v1/orgs/{id}/entitlement-overrides`）。
- **验收**：
  - `go test -tags integration ./...`
  - `go test ./internal/entitlement/...`：Resolve 方法覆盖三级合并逻辑（override > plan > 默认值）、过期 override 自动回退。
  - 手工：创建 Plan（`quota.runs_per_month=10`）-> org 订阅该 Plan -> 查询 `EntitlementService.Resolve(orgID, "quota.runs_per_month")` 返回 10 -> 加 override 改为 20 -> Resolve 返回 20。

### R62.2 -- 用量计量（Usage Metering）

- **关联审计**：六「订阅与计费不存在」
- **关联目标架构**：六.6.14 `CREATE TABLE usage_records`
- **依赖**：R30（Worker 终态写入 + usage 累加）
- **目标**：建立计量层（Metering），记录每次 LLM 调用的 token 用量和成本。不可变事件流，只记录事实，不关心 Plan 和限额。

- **关键点**：
  - `usage_records`：每次 run 的 token 用量 + 成本，关联 `run_id` + `org_id`。字段包括 `id`、`org_id`、`run_id`、`model`、`input_tokens`、`output_tokens`、`cost_usd`、`recorded_at`。
  - Worker 终态写入时同事务写 `usage_records`（从 R30 的 usage 累加数据中取）。
  - `usage_records` 按 `(org_id, recorded_at)` 索引支撑月度汇总查询。
  - 提供汇总查询方法：`GetMonthlyUsage(ctx, orgID, year, month) -> UsageSummary`，返回当月总 token 数和总成本。

- **具体改动范围**：
  - 新建 migration `00033_create_usage_records.sql`：`usage_records` 表。
  - 新建 `src/services/api/internal/data/usage_repo.go`：CRUD + 月度汇总查询。
  - 修改 Worker 终态逻辑（`src/services/worker/internal/runengine/` 或对应 Pipeline handler）：写入 `usage_records`。
  - 新建 `src/services/api/internal/http/v1_usage.go`：用量查询端点（`GET /v1/orgs/{id}/usage`）。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：run 完成后 `SELECT * FROM usage_records WHERE run_id = $1` 有记录。
  - 手工：查询月度汇总返回正确的 token 和成本总计。

### R62.3 -- 配额执行（Quota Enforcement）

- **关联审计**：六「订阅与计费不存在」
- **关联目标架构**：六.6.14 权益执行层
- **依赖**：R62.1（EntitlementService）；R62.2（usage_records，用于用量配额检查）；R46（Pipeline 架构）
- **目标**：建立执行层（Enforcement），在 Run 和 API 两个层面检查配额。执行层不知道用户是什么 Plan、不知道价格，只做一件事：比较当前用量和允许上限。

- **关键点**：
  - **Run 级检查（Pipeline Middleware）**：新建 `EntitlementMiddleware` 插入 R46 的 Pipeline（位于 InputLoaderMiddleware 之后、MCPDiscoveryMiddleware 之前）。Run 开始前检查 `quota.runs_per_month`、`limit.concurrent_runs`、`feature.byok_enabled`（如果 run 使用 BYOK 凭证）等。超限时写入 `run.failed` 事件（error_class: `entitlement.quota_exceeded`）并短路 Pipeline。
  - **API 级检查（HTTP Middleware）**：在 API 的 HTTP middleware 中检查请求级限额（如 `limit.team_members` 在创建 team member 时校验）。复用 `EntitlementService.Resolve`。
  - R44 的并发 Run 限制（Redis counter `arkloop:org:active_runs`）纳入权益体系：默认值从环境变量改为读取 `limit.concurrent_runs` 权益值，保留 Redis counter 机制不变。
  - `quota.runs_per_month` 检查逻辑：从 `usage_records` 汇总当月 run 数量，与权益值比较。
  - `quota.tokens_per_month` 检查逻辑：从 `usage_records` 汇总当月 token 数量，与权益值比较。

- **具体改动范围**：
  - 新建 `src/services/worker/internal/pipeline/mw_entitlement.go`：`EntitlementMiddleware`，Run 级额度检查。
  - 修改 `src/services/worker/internal/app/composition.go`：将 `EntitlementMiddleware` 注册到 Pipeline。
  - 新建 `src/services/api/internal/http/middleware_entitlement.go`：API 级请求限额检查。
  - 修改 `src/services/api/internal/http/v1_runs.go`：并发 Run 限制从环境变量切换到读取 `limit.concurrent_runs` 权益值。
- **验收**：
  - `go test -tags integration ./...`
  - `go test ./internal/pipeline/...`：EntitlementMiddleware 单测覆盖超限拒绝、override 优先级、过期 override 回退。
  - 手工：创建 Plan（`quota.runs_per_month=10`）-> org 订阅该 Plan -> 创建第 11 个 run 被拒绝 -> 后台加 override（`quota.runs_per_month=20`）-> 第 11 个 run 成功。

### R63 -- Feature Flags

- **关联审计**：六「Feature Flags 不存在」；P2
- **关联目标架构**：六.6.16 `CREATE TABLE feature_flags` + `org_feature_overrides`
- **依赖**：R20（Redis 缓存）
- **目标**：创建 `feature_flags` + `org_feature_overrides` 表，实现全局 + org 级 feature flag。
- **关键点**：
  - `feature_flags`：`key TEXT UNIQUE`、`description`、`default_value BOOLEAN`。
  - `org_feature_overrides`：`(org_id, flag_key) PRIMARY KEY`，覆盖全局默认。
  - Redis 缓存：`arkloop:feat:{org_id}:{flag}` TTL 5min。
  - Go 侧提供 `FeatureFlagService.IsEnabled(ctx, orgID, flagKey) bool`。
- **具体改动范围**：
  - 新建 migration `00034_create_feature_flags.sql`
  - 新建 `src/services/api/internal/data/feature_flags_repo.go`
  - 新建 `src/services/api/internal/http/v1_feature_flags.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 flag -> 设置 org override -> 验证生效。

### R64 -- 通知系统

- **关联审计**：六「通知系统不存在」；P2
- **关联目标架构**：六.6.20 `CREATE TABLE notifications`
- **目标**：创建 `notifications` 表，实现站内通知（run 完成通知）。
- **关键点**：
  - `notifications`：`id`、`user_id`、`org_id`、`type`、`title`、`body`、`payload_json`、`read_at`、`created_at`。
  - 创建时机：Worker 写入 run 终态事件后创建通知。
  - API 端点：
    - `GET /v1/notifications`（未读列表）
    - `PATCH /v1/notifications/{id}`（标记已读）
  - 未来可扩展为 email/push 通知。
- **具体改动范围**：
  - 新建 migration `00035_create_notifications.sql`
  - 新建 `src/services/api/internal/data/notifications_repo.go`
  - 新建 `src/services/api/internal/http/v1_notifications.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：run 完成后查询通知列表有记录。

---

## 8.5 Phase 6.5 -- Console Management

目标：为 Phase 1-6 的全部后端功能提供 Web 管理界面（`src/apps/console/`）。Console 是运营和调试的入口，聚焦 CRUD 操作面，不做 SSE 实时流。分三个子阶段交付：6.5a（共享层 + 运营核心面）、6.5b（配置管理面）、6.5c（集成/安全/组织/计费/平台）。

**为什么在 Phase 7 之前做：**

Phase 6 完成后，后端已覆盖 16+ 资源域的完整 API，但 Console 停留在全 placeholder 状态。没有管理界面意味着：无法验证后端功能是否正确运行、无法在不写 cURL 的情况下配置 Agent、无法查看 Usage/通知/审计。Phase 7 的性能优化可以并行推进，但可操作性不能后置，因为它是 Phase 7 开发过程中的工具。

**技术约定：**

- **不引入第三方 UI 组件库**。手写 Tailwind 共享组件（~300 行），复用现有 CSS 变量体系。
- **API Client 层**：为每个资源域建立 typed fetch namespace（`src/apps/console/src/api/*.ts`），复用现有 `apiFetch` 基础。
- **页面模式标准化**：列表页 = PageHeader + DataTable；创建/编辑 = Modal + FormField；删除 = ConfirmDialog。

**导航分组结构（7 组）：**

```
Operations:     Runs | Audit Logs | Notifications
Configuration:  Agent Configs | LLM Credentials | MCP Configs | Skills | Prompt Templates
Integration:    Webhooks | API Keys
Security:       IP Rules
Organization:   Teams | Projects | Members
Billing:        Plans | Subscriptions | Entitlements | Usage
Platform:       Feature Flags
```

### 前置工作 -- 后端端点补全

#### R65 -- Audit Log List 端点

- **背景**：当前 audit 只有 `Writer`（写入）和 `AuditLogRepository.Create`，缺少 List 查询端点。
- **目标**：新增 `GET /v1/audit-logs` 端点，支持按 org、action、actor、时间范围过滤，分页返回。
- **关键点**：
  - 查询参数：`org_id`（必填）、`action`、`actor_user_id`、`target_type`、`since`、`until`、`limit`、`offset`。
  - 返回字段：id、org_id、actor_user_id、action、target_type、target_id、trace_id、metadata_json、ip_address、user_agent、created_at。
  - 不返回 before_state_json / after_state_json（可选 `?include=state` 参数展开）。
  - 权限：需要 `platform.admin` 或 `org.audit_read`。
- **具体改动范围**：
  - 修改 `src/services/api/internal/data/audit_logs_repo.go`：新增 `List` 方法（带过滤条件的分页查询）。
  - 新建 `src/services/api/internal/http/v1_audit_logs.go`：handler。
  - 修改 `src/services/api/internal/http/handler.go`：注册 `/v1/audit-logs` 路由。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：执行几个操作 -> 查询审计列表返回正确记录，过滤条件生效。

#### R66 -- 全局 Runs List 端点

- **背景**：当前 runs list 只能按 thread 查询（`/v1/threads/{id}/runs`），管理后台需要跨 thread/org 的全局视图。
- **目标**：新增 `GET /v1/runs` 的全局查询能力（当前 `/v1/runs/` 只处理子路径）。
- **关键点**：
  - 查询参数：`org_id`（平台管理员可省略查全局）、`status`、`since`、`until`、`limit`、`offset`。
  - 返回字段：id、thread_id、org_id、status、model、skill_id、total_input_tokens、total_output_tokens、total_cost_usd、duration_ms、created_at、completed_at、failed_at。
  - 权限：平台管理员查全局，org 成员只能查自己 org。
- **具体改动范围**：
  - 修改 `src/services/api/internal/data/runs_repo.go`（或 `run_events_repo.go`）：新增 `ListRuns` 方法（跨 thread 查询）。
  - 修改 `src/services/api/internal/http/v1_runs.go`：在 `runEntry` 或独立 handler 中处理 `GET /v1/runs` 无子路径的情况。
  - 修改 `src/services/api/internal/http/handler.go`：注册路由。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：查询返回跨 thread 的 run 列表，过滤条件生效。

### Phase 6.5a -- 共享组件层 + 运营核心面

#### R67.1 -- Console 共享组件与导航重构

- **目标**：建立 Console 的 UI 组件基础设施，重构 sidebar 导航为 7 组分组结构。
- **关键点**：
  - **共享组件**（全手写 Tailwind，复用现有 `--c-*` CSS 变量）：
    - `DataTable<T>` -- 泛型列定义、排序、空状态、loading 状态
    - `Modal` -- portal + overlay + escape 关闭 + click outside
    - `ConfirmDialog` -- 删除确认（Modal 变体，传入 message + onConfirm）
    - `FormField` -- label + input/select/toggle/textarea + error hint
    - `Badge` -- 状态标签（variant: success/warning/error/neutral）
    - `PageHeader` -- 标题 + 右侧操作按钮区
    - `EmptyState` -- icon + message
    - `Toast` -- portal + auto-dismiss + 堆叠管理（context provider）
  - **导航重构**：`ConsoleLayout.tsx` 的 `NAV_ITEMS` 从平铺数组改为分组结构，sidebar 支持 section 折叠/展开。每个 section 用分隔线 + 小标题标识。
  - **API Client 拆分**：`src/apps/console/src/api/` 目录下按资源域拆分（`runs.ts`、`notifications.ts`、`audit.ts` 等），公用 `apiFetch` 从 `api.ts` 导出。
  - **路由注册**：`App.tsx` 注册全部页面路由（placeholder 替换为实际组件，未实现的先保持 placeholder）。
- **具体改动范围**：
  - 新建 `src/apps/console/src/components/` 目录及上述 8 个组件文件。
  - 修改 `src/apps/console/src/layouts/ConsoleLayout.tsx`：导航分组。
  - 新建 `src/apps/console/src/api/` 目录。
  - 修改 `src/apps/console/src/App.tsx`：路由表。
- **验收**：
  - `pnpm build` 无报错。
  - 导航分组正确显示，折叠/展开正常。

#### R67.2 -- Runs 页面

- **依赖**：R66（全局 Runs List 端点）、R67.1（共享组件）
- **目标**：列表（全局视图）+ 基本信息展示 + cancel 操作。
- **关键点**：
  - 列表列：ID（截断显示）、Thread ID（截断）、Status（Badge）、Model、Duration、Tokens（input/output）、Cost、Created At。
  - 状态过滤：下拉选择 running/completed/failed/cancelled。
  - Cancel 操作：ConfirmDialog 确认后 POST。
  - 手动刷新按钮获取最新状态，不做 SSE 实时流。
- **具体改动范围**：
  - 替换 `src/apps/console/src/pages/RunsPage.tsx`。
  - 新建 `src/apps/console/src/api/runs.ts`。

#### R67.3 -- Notifications 页面

- **依赖**：R67.1（共享组件）
- **目标**：未读列表 + 标记已读。
- **关键点**：
  - 列表：Type（Badge）、Title、Body、Created At。
  - 单条标记已读：按钮 PATCH。
  - Sidebar 导航 Notifications 旁显示未读数量 badge（ConsoleLayout 轮询或初始加载时获取）。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/notifications/NotificationsPage.tsx`。
  - 新建 `src/apps/console/src/api/notifications.ts`。
  - 修改 `ConsoleLayout.tsx`：未读计数展示。

#### R67.4 -- Audit Logs 页面

- **依赖**：R65（Audit List 端点）、R67.1（共享组件）
- **目标**：列表 + 过滤。
- **关键点**：
  - 列表列：Action、Actor User ID（截断）、Target Type/ID、IP Address、Trace ID、Created At。
  - 过滤：Action 类型下拉、时间范围选择。
  - 行展开：点击查看 metadata_json 详情。
- **具体改动范围**：
  - 替换 `src/apps/console/src/pages/AuditPage.tsx`。
  - 新建 `src/apps/console/src/api/audit.ts`。

### Phase 6.5b -- 配置管理面

#### R67.5 -- LLM Credentials 页面

- **依赖**：R67.1（共享组件）
- **目标**：列表 + 创建 + 删除，含路由配置。
- **关键点**：
  - 列表列：Name、Provider（Badge）、Key Prefix、Base URL、OpenAI API Mode、Routes Count、Created At。
  - 创建 Modal：Name、Provider（select: openai/anthropic/gemini/deepseek）、API Key（password input）、Base URL（optional）、OpenAI API Mode（optional select）、Routes（动态添加行：model/priority/is_default/when JSON）。
  - 删除：ConfirmDialog。
  - API Key 永远不展示明文（后端只返回 key_prefix）。
- **具体改动范围**：
  - 替换 `src/apps/console/src/pages/ProvidersPage.tsx` 为 `src/apps/console/src/pages/credentials/CredentialsPage.tsx`。
  - 新建 `src/apps/console/src/api/llm-credentials.ts`。

#### R67.6 -- Agent Configs 页面

- **依赖**：R67.1（共享组件）
- **目标**：列表 + 创建 + 编辑 + 删除。
- **关键点**：
  - 列表列：Name、Model、Temperature、Max Output Tokens、Tool Policy、Is Default（Badge）、Project（if set）、Created At。
  - 创建/编辑 Modal：Name、System Prompt Template（select 现有 prompt templates）、System Prompt Override（textarea）、Model（text）、Temperature（number）、Max Output Tokens（number）、Top P（number）、Tool Policy（select: auto/required/none）、Tool Allowlist（逗号分隔 text）、Tool Denylist（逗号分隔 text）、Content Filter Level（text）、Is Default（toggle）。
  - 删除：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/agent-configs/AgentConfigsPage.tsx`。
  - 新建 `src/apps/console/src/api/agent-configs.ts`。

#### R67.7 -- Prompt Templates 页面

- **依赖**：R67.1（共享组件）
- **目标**：列表 + 创建 + 编辑 + 删除。
- **关键点**：
  - 列表列：Name、Is Default（Badge）、Version、Variables Count、Created At。
  - 创建/编辑 Modal：Name、Content（textarea，支持 `{{variable}}` 语法）、Variables（逗号分隔 text）、Is Default（toggle）。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/prompt-templates/PromptTemplatesPage.tsx`。
  - 新建 `src/apps/console/src/api/prompt-templates.ts`。

#### R67.8 -- MCP Configs 页面

- **依赖**：R67.1（共享组件）
- **目标**：列表 + 创建 + 编辑 + 删除。
- **关键点**：
  - 列表列：Server ID、Display Name、Transport（Badge: http_sse/streamable_http/stdio）、Enabled（Badge）、Created At。
  - 创建/编辑 Modal：Server ID、Display Name、Transport（select）、URL（http_sse/streamable_http 时显示）、Command/Args/Env（stdio 时显示）、Auth Header Name/Value（optional password input）、Enabled（toggle）。
  - 表单根据 transport 选择动态显示/隐藏字段。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/mcp-configs/MCPConfigsPage.tsx`。
  - 新建 `src/apps/console/src/api/mcp-configs.ts`。

#### R67.9 -- Skills 页面

- **依赖**：R67.1（共享组件）
- **目标**：列表 + 创建 + 编辑。
- **关键点**：
  - 列表列：Skill Key、Display Name、Version、Is Active（Badge）、Created At。
  - 创建/编辑 Modal：Skill Key、Display Name、Description（text）、Prompt（textarea）、Tool Allowlist（逗号分隔 text）、Budgets JSON（textarea）、Is Active（toggle）。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/skills/SkillsPage.tsx`。
  - 新建 `src/apps/console/src/api/skills.ts`。

### Phase 6.5c -- 集成 / 安全 / 组织 / 计费 / 平台

#### R68.1 -- API Keys 页面

- **关键点**：
  - 列表列：Key Prefix、Name、Scopes、Last Used At、Expires At、Created At。
  - 创建 Modal：Name、Scopes（multi-select or text）、Expires At（optional）。创建成功后 Modal 显示一次性明文 key，要求用户复制。
  - 吊销：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/api-keys/APIKeysPage.tsx`。
  - 新建 `src/apps/console/src/api/api-keys.ts`。

#### R68.2 -- Webhooks 页面

- **关键点**：
  - 列表列：URL、Events（Badge 列表）、Enabled（Badge）、Created At。
  - 创建 Modal：URL、Events（checkbox: run.completed/run.failed/run.cancelled）。
  - 编辑：只能 toggle Enabled 状态（PATCH）。
  - 删除：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/webhooks/WebhooksPage.tsx`。
  - 新建 `src/apps/console/src/api/webhooks.ts`。

#### R68.3 -- IP Rules 页面

- **关键点**：
  - 列表列：Type（allowlist/blocklist Badge）、CIDR、Note、Created At。
  - 创建 Modal：Type（select）、CIDR（text）、Note（optional text）。
  - 删除：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/ip-rules/IPRulesPage.tsx`。
  - 新建 `src/apps/console/src/api/ip-rules.ts`。

#### R68.4 -- Teams 页面

- **关键点**：
  - 列表列：Name、Members Count、Created At。
  - 创建 Modal：Name。
  - 详情页（或行展开）：成员列表（User ID、Role、Created At）+ 添加成员 Modal（User ID、Role select）+ 移除成员 ConfirmDialog。
  - 删除 Team：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/teams/TeamsPage.tsx`。
  - 新建 `src/apps/console/src/api/teams.ts`。

#### R68.5 -- Projects 页面

- **关键点**：
  - 列表列：Name、Team（if set）、Visibility（Badge: private/team/org）、Created At。
  - 创建 Modal：Name、Description、Team（select，optional）、Visibility（select）。
  - 编辑：PATCH name/description/visibility。
  - 删除：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/projects/ProjectsPage.tsx`。
  - 新建 `src/apps/console/src/api/projects.ts`。

#### R68.6 -- Members & Invitations 页面

- **关键点**：
  - 两个 tab：Current Members / Pending Invitations。
  - Members tab：列表（User ID、Display Name、Role、Joined At）。
  - Invitations tab：列表（Email、Role、Token 截断、Expires At、Status）。
  - 创建邀请 Modal：Email、Role（select）。
  - 撤回邀请：ConfirmDialog + DELETE。
- **具体改动范围**：
  - 替换 `src/apps/console/src/pages/OrgsPage.tsx` 为 `src/apps/console/src/pages/members/MembersPage.tsx`。
  - 新建 `src/apps/console/src/api/org-invitations.ts`。

#### R68.7 -- Plans 页面

- **关键点**：
  - 列表列：Name、Display Name、Entitlements Count、Created At。
  - 创建 Modal：Name、Display Name、Entitlements（动态添加行：Key、Value、Value Type select: int/bool/string）。
  - 详情展开：显示该 Plan 的全部 entitlements 列表。
  - 删除：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/plans/PlansPage.tsx`。
  - 新建 `src/apps/console/src/api/plans.ts`。

#### R68.8 -- Subscriptions 页面

- **关键点**：
  - 列表列：Org ID（截断）、Plan Name、Status（Badge: active/cancelled/expired）、Period Start/End、Created At。
  - 创建 Modal：Org ID、Plan（select 已有 plans）、Period Start（date）、Period End（date）。
  - Cancel：ConfirmDialog -> PATCH status=cancelled。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/subscriptions/SubscriptionsPage.tsx`。
  - 新建 `src/apps/console/src/api/subscriptions.ts`。

#### R68.9 -- Entitlement Overrides 页面

- **关键点**：
  - 列表列：Org ID（截断）、Key、Value、Value Type（Badge）、Reason、Expires At、Created By、Created At。
  - 创建 Modal：Org ID、Key（text）、Value（text）、Value Type（select: int/bool/string）、Reason（optional textarea）、Expires At（optional datetime）。
  - 删除：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/entitlements/EntitlementsPage.tsx`。
  - 新建 `src/apps/console/src/api/entitlements.ts`。

#### R68.10 -- Usage 页面

- **关键点**：
  - Org 选择器（text input for org_id）+ Year/Month 选择器。
  - 展示卡片（四宫格）：Total Input Tokens、Total Output Tokens、Total Cost USD、Record Count。
  - 不做图表（后续可扩展）。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/usage/UsagePage.tsx`。
  - 新建 `src/apps/console/src/api/usage.ts`。

#### R68.11 -- Feature Flags 页面

- **关键点**：
  - 列表列：Key、Description、Default Value（Badge: on/off）、Overrides Count、Created At。
  - 创建 Modal：Key、Description（optional）、Default Value（toggle）。
  - 详情展开/子页：Org Overrides 列表（Org ID、Enabled Badge、Created At）。
  - 添加 Override Modal：Org ID、Enabled（toggle）。
  - 删除 Override：ConfirmDialog。
  - 删除 Flag：ConfirmDialog。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/feature-flags/FeatureFlagsPage.tsx`。
  - 新建 `src/apps/console/src/api/feature-flags.ts`。

---

## 8.6 Phase 6.6 -- 双 Token 认证强化

目标：将当前单一 Access Token 模式升级为 Access Token + Refresh Token 双 Token 体系，实现「只要用户持续活跃就永不掉线」的用户体验，同时保持对 token 吊销（强制下线、账号封禁）的精确控制能力。

**背景与动机：**

当前认证机制为纯无状态 JWT Access Token，TTL 默认 3600 秒（已调整为 30 天作为过渡）。单 Token 模式存在两个根本矛盾：TTL 短则用户频繁掉线，TTL 长则 token 泄漏后吊销窗口过大。对于面向外部用户的 SaaS，两者都不可接受。

代码库中 `/v1/auth/refresh` 路由和 `Service.RefreshAccessToken()` 方法已存在，但实现有缺陷：当前 refresh 接受 Access Token 作为输入，一旦 Access Token 过期即无法完成刷新，语义等同于「续命」而非真正的 Refresh Token。本 Phase 在已有骨架上做语义替换，不重建路由。

**设计决策：**

- Access Token TTL：15 分钟（短命，泄漏影响面小）
- Refresh Token TTL：90 天（长命，持续活跃则自动续）
- Refresh Token 存储：DB 表 `refresh_tokens`，存哈希值；每次 refresh 原子性轮换（旧 token 作废、签发新 token），防重放攻击
- Refresh Token 形式：随机 32 字节，base64url 编码；不用 JWT，避免无法在 DB 侧主动失效
- 前端静默刷新：`apiFetch` 拦截 401 → 用 Refresh Token 换新 Access Token → 自动重试原请求；并发 401 时只触发一次 refresh，其余请求排队等结果

### R69.1 -- 双 Token 后端基础设施

- **依赖**：无新依赖，在现有 auth 包上修改
- **目标**：
  - 新建 `refresh_tokens` 表，提供 Refresh Token 的签发、验证、轮换、吊销能力
  - 重写 `Service.IssueAccessToken` 同时返回 Access + Refresh Token
  - 重写 `/v1/auth/refresh` 端点：改为接受 Refresh Token（请求体），不再接受 Access Token（Bearer header）；完成原子轮换后返回新的两个 token
  - 更新 `Logout`：同时吊销用户所有 Refresh Token
  - 将 Access Token TTL 默认值改回 900 秒（15 分钟）；新增 `RefreshTokenTTLSeconds` 默认值 7776000（90 天）
- **关键点**：
  - `refresh_tokens` 表字段：`id UUID PK`、`user_id UUID NOT NULL`、`token_hash TEXT NOT NULL UNIQUE`（SHA-256，hex）、`expires_at TIMESTAMPTZ NOT NULL`、`revoked_at TIMESTAMPTZ`、`created_at TIMESTAMPTZ`、`last_used_at TIMESTAMPTZ`。索引：`user_id`、`token_hash`。
  - **轮换语义**（原子操作，单事务）：`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $old AND revoked_at IS NULL AND expires_at > now()` → 若更新行数为 0 则返回 `TokenInvalidError`（检测重放）→ `INSERT INTO refresh_tokens` 写入新 token → 签发新 Access Token。
  - `Logout` 逻辑：`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`，同时保留现有 `BumpTokensInvalidBefore` 逻辑使所有存量 Access Token 一并失效。
  - `/v1/auth/refresh` 请求体：`{ "refresh_token": "..." }`；响应体：`{ "access_token": "...", "refresh_token": "...", "token_type": "bearer" }`。
  - `/v1/auth/login` 和 `/v1/auth/register` 响应体新增 `refresh_token` 字段。
  - 定期清理：`refresh_tokens` 表中过期且已吊销的记录可通过后台 job 清理（留给运维，本 R 不实现 job，只确保查询走索引）。
- **具体改动范围**：
  - 新建 `src/services/api/internal/migrate/migrations/00044_create_refresh_tokens.sql`
  - 新建 `src/services/api/internal/data/refresh_tokens_repo.go`：`Create`、`GetByHash`（校验 revoked_at IS NULL AND expires_at > now）、`RevokeByHash`、`RevokeAllForUser`
  - 修改 `src/services/api/internal/auth/config.go`：`defaultAccessTokenTTL` 改为 900；新增 `RefreshTokenTTLSeconds`（默认 7776000）；`Config` 结构加 `RefreshTokenTTLSeconds int`；`Validate` 补检查
  - 修改 `src/services/api/internal/auth/tokens.go`：新增 `IssueRefreshToken(userID uuid.UUID) (plaintext string, hash string, expiresAt time.Time, error)` — `crypto/rand` 生成 32 字节，base64url 编码为 plaintext，`sha256` 计算 hash
  - 修改 `src/services/api/internal/auth/service.go`：`Service` 结构加 `refreshTokenRepo`；`IssueAccessToken` 返回新类型 `IssuedTokenPair{AccessToken, RefreshToken}`；新增 `ConsumeRefreshToken(ctx, plaintext) (IssuedTokenPair, error)` 实现轮换；`Logout` 调用 `RevokeAllForUser`
  - 修改 `src/services/api/internal/http/v1_auth.go`：更新 `loginResponse` / `registerResponse` 类型加 `refresh_token`；重写 `refreshToken` handler（从 body 读而非 header）；更新 `login` / `register` handler 传递新的 `IssuedTokenPair`
  - 修改 `src/services/api/internal/http/handler.go`：`HandlerConfig` 加 `RefreshTokenRepo`，传入 `refreshToken` handler
  - 修改 `src/services/api/internal/app/app.go`：初始化 `RefreshTokenRepository`，注入 handler
  - 修改 `.env.example`：`ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS` 改为 900，新增 `ARKLOOP_AUTH_REFRESH_TOKEN_TTL_SECONDS=7776000`
- **验收**：
  - `go test -tags integration ./...`
  - `go test ./internal/auth/...`：覆盖轮换重放检测（旧 token 被二次使用返回 401）、过期 refresh token 返回 401、logout 后 refresh 返回 401。
  - 手工：登录获取两个 token → 等待 Access Token 过期（本地测试可将 TTL 设为 5s）→ 用 Refresh Token 调 `/v1/auth/refresh` 获得新 token 对 → 旧 Refresh Token 再次调用返回 401（轮换生效）。

### R69.2 -- 双 Token 前端静默刷新

- **依赖**：R69.1（后端 Refresh Token 接口）
- **目标**：
  - Web App（`src/apps/web/`）和 Console（`src/apps/console/`）均实现静默刷新：Access Token 过期时自动换新，用户无感知；Refresh Token 失效时才跳登录页
  - 解决并发 401 竞态：多个请求同时 401，只执行一次 refresh，其余请求挂起等待结果后重试
- **关键点**：
  - **存储**：`storage.ts`（两个 app 各自）新增 `readRefreshTokenFromStorage` / `writeRefreshTokenToStorage` / `clearRefreshTokenFromStorage`，key 为 `arkloop:web:refresh_token` / `arkloop:console:refresh_token`。
  - **并发控制**：在 `api.ts` 模块级维护一个 `refreshPromise: Promise<string> | null` 变量。收到 401 时：若 `refreshPromise` 已存在则等待它；否则发起 refresh 并将 promise 存入，完成后置 null。这样 N 个并发 401 只触发一次 refresh 请求。
  - **拦截逻辑**（`apiFetch` 改造）：
    1. 正常发请求
    2. 若响应 401 且存在 Refresh Token → 进入静默刷新流程（见并发控制）
    3. refresh 成功 → 更新内存中的 access token → 用新 token 重试原请求（只重试一次，防死循环）
    4. refresh 失败（401/403）→ 清除所有 token 存储 → 调用全局 `onUnauthenticated` 回调（跳登录页）
  - **全局回调注册**：`api.ts` 导出 `setUnauthenticatedHandler(fn: () => void)`，在 `App.tsx` 的 mount 阶段调用，传入 `handleLoggedOut`。这样 `apiFetch` 不直接依赖 React，保持可测试性。
  - **token 同步**：`apiFetch` 调用 `refresh` 后须同时更新传给 React 状态的 access token。通过 `setAccessTokenHandler` 全局回调（与 `setUnauthenticatedHandler` 同模式）注入，`App.tsx` 在 mount 时注册。
  - SSE 连接（`sse.ts`）：SSE 使用 URL 参数传 token，refresh 后需重建连接；`useSSE` hook 已有 `reconnect` 接口，refresh 成功后调用即可（由 `sse.ts` 层处理 401 事件触发）。
- **具体改动范围**（web 和 console 各做一份，下述为 web，console 对称）：
  - 修改 `src/apps/web/src/storage.ts`：新增 refresh token 的 read / write / clear
  - 修改 `src/apps/web/src/api.ts`：
    - `LoginResponse` / `RegisterResponse` 加 `refresh_token` 字段
    - 新增 `refreshAccessToken(refreshToken: string): Promise<LoginResponse>` 函数
    - 新增模块级 `let refreshPromise: Promise<string> | null = null`
    - 新增 `setUnauthenticatedHandler` / `setAccessTokenHandler` 注册函数
    - `apiFetch` 加 401 拦截 + 静默刷新 + 原请求重试逻辑
  - 修改 `src/apps/web/src/App.tsx`：登录/注册时保存 refresh token；mount 时注册 `setUnauthenticatedHandler` / `setAccessTokenHandler`；登出时清除 refresh token
  - 修改 `src/apps/console/src/storage.ts`：同上
  - 修改 `src/apps/console/src/api/auth.ts`（或等效文件）：同上
  - 修改 `src/apps/console/src/App.tsx`：同上
- **验收**：
  - `pnpm --filter web tsc --noEmit` / `pnpm --filter console tsc --noEmit` 通过
  - 手工（web）：登录 → 将 localStorage 中的 access token 替换为已过期的合法 JWT → 发任意请求 → 观察 Network 面板：自动出现一次 `/v1/auth/refresh` 请求，原请求自动重试并成功，用户无感知跳转
  - 手工（web）：清除 refresh token → 发任意请求 → 被跳转到登录页
  - 手工（并发）：同时触发 3 个 API 请求（均 401）→ Network 面板确认只有一次 refresh 请求

---

## 9. Phase 7 -- 性能与可扩展性

目标：解决 Phase 1-6 功能完成后暴露的扩展性瓶颈，使系统能支撑 50+ Worker 实例、1000+ 并发 Run、多 API 实例部署。

关联文档：`src/docs/architecture/architecture-problems.zh-CN.md` 第九节「新发现的扩展性盲点」

### R70 -- PgBouncer 连接池代理

- **关联审计**：九.9.1「没有数据库连接池代理」
- **目标**：在 PostgreSQL 和应用层之间引入 PgBouncer（transaction mode），将数千应用连接复用为几百个实际数据库连接。
- **关键点**：
  - PgBouncer 以 transaction mode 运行：每个事务结束后连接立即归还到池中，不绑定到特定客户端。
  - API、Worker、Gateway（如果后续直连 DB）的 DSN 全部改为指向 PgBouncer，不再直连 PostgreSQL。
  - PgBouncer `default_pool_size` 建议 200（可根据 PostgreSQL 实际 `max_connections` 调整）。
  - `pgxpool` 的应用层连接池保留，但 `MaxConns` 可以调高（因为实际连接由 PgBouncer 控制）。
  - **已知限制**：transaction mode 下 `LISTEN/NOTIFY` 不可用（因为 LISTEN 需要持有连接）。API 的 SSE handler 当前使用 `pool.Acquire()` + `LISTEN`，需要为此保留一条直连通道（PgBouncer 之外单独配一个 direct DSN 或用 `session` mode 的小池子专供 LISTEN）。
  - 健康检查：PgBouncer 自带 `SHOW STATS` 和 `SHOW POOLS` 管理命令。
- **具体改动范围**：
  - 修改 `compose.yaml`：新增 `pgbouncer` service（`edoburu/pgbouncer` 或 `bitnami/pgbouncer` 镜像）。
  - 新增 `src/services/pgbouncer/pgbouncer.ini`（或通过环境变量配置）。
  - 修改 `.env.example`：新增 `ARKLOOP_PGBOUNCER_URL`，调整 `DATABASE_URL` 指向 PgBouncer。
  - 修改 API SSE handler：对 LISTEN 连接使用直连 DSN（不经过 PgBouncer）。
  - Worker 的 `DATABASE_URL` 指向 PgBouncer。
- **验收**：
  - `docker compose up -d` 后 API + Worker 通过 PgBouncer 正常工作。
  - `go test -tags integration ./...` 全绿。
  - 手工：启动 20 个 Worker 实例（`docker compose scale worker=20`），观察 PgBouncer `SHOW POOLS` 实际连接数远小于应用连接数。
  - 手工：SSE 推送仍然通过 LISTEN/NOTIFY 实时到达（直连通道生效）。

### R71 -- run_events 月分区

- **关联审计**：九.9.2「run_events 无月分区」；原始审计二.2.5
- **目标**：将 `run_events` 表改为按 `ts` 的 RANGE 月分区，支持旧分区归档和 DROP。
- **关键点**：
  - 创建新的分区表 `run_events_partitioned`，结构与现有 `run_events` 一致。
  - 预创建当前月 + 下月的分区。
  - 数据迁移策略：
    1. 创建分区表和索引
    2. 后台批量搬迁旧数据（按 `ts` 范围，每批 10,000 行，避免锁表）
    3. 双写期：新事件同时写入旧表和分区表
    4. 切换：应用层读写全部切到分区表
    5. 旧表在确认无残留读取后 DROP
  - 分区管理 cron：每月 25 日自动创建下下月分区（预留缓冲）。
  - 归档：超过 3 个月的分区导出到对象存储（JSONL.gz），DETACH + DROP。
  - `run_events_seq_global` sequence 保留（分区表继续使用）。
  - 索引策略：每个分区独立创建 `(run_id, seq)` 索引（分区本地索引），不用全局索引。
- **具体改动范围**：
  - 新建 migration：创建分区表 + 当前月/下月分区。
  - 新建 `src/services/api/internal/data/partition_manager.go`：分区自动创建逻辑。
  - 修改 `run_events_repo.go`（API 和 Worker 两侧）：写入目标改为分区表。
  - 新建归档脚本（可做成后台 job 或独立 CLI 命令）。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：`\d+ run_events_partitioned` 确认分区存在。
  - 手工：创建 run，事件写入正确分区。
  - 手工：SSE 读取跨月事件不报错。

### R72 -- SSE 多实例 Redis Pub/Sub 广播

- **关联审计**：九.9.3「SSE 多实例广播缺失」
- **关联目标架构**：五.5.2 多实例同步
- **依赖**：R20（Redis 已引入）
- **目标**：Worker commit 事件后同时发 `pg_notify` 和 Redis Pub/Sub。API 的 SSE handler 订阅 Redis channel 作为跨实例信号源。
- **关键点**：
  - Worker 写入事件 commit 后：
    1. `pg_notify('run_events:{run_id}', seq)` -- 保留，快速路径
    2. `redis.Publish(ctx, 'arkloop:sse:run_events:{run_id}', seq)` -- 新增，跨实例广播
  - API SSE handler 同时监听两个信号源：
    - pg_notify（同实例快速路径，延迟最低）
    - Redis Pub/Sub（跨实例广播，保证所有 API 实例都能收到）
  - 任一信号触发后查库推送，去重靠 `cursor`（seq 游标），不会重复推送。
  - Redis Pub/Sub 不持久化消息（fire-and-forget），SSE handler 的兜底仍是心跳周期内查库。
- **具体改动范围**：
  - 修改 `src/services/worker/internal/runengine/v1.go`：commit 后新增 Redis Publish。
  - 修改 `src/services/api/internal/http/v1_runs.go`：SSE handler 新增 Redis Subscribe，与 pg_notify 合并为 select 多路复用。
  - Worker 和 API 的 Redis client 已在 R20 引入，复用即可。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：启动 2 个 API 实例，用户 SSE 连接到 API-A，Worker 事件推送通过 API-B 也能到达（Redis Pub/Sub 路径）。

### R73 -- 并发 Run 计数器泄漏修复

- **关联审计**：九.9.5「Worker 崩溃时 Redis 并发计数器泄漏」
- **依赖**：R44（并发 Run 限制已实现）
- **目标**：实现定时同步机制，防止 Worker 崩溃导致计数器永久偏高。
- **关键点**：
  - 后台 goroutine（在 API 服务中运行，每 60 秒一次）：
    1. 扫描 `runs` 表中 `status='running' AND updated_at < now() - interval '5 minutes'` 的 run
    2. 将这些 run 标记为 `status='failed'`，`failed_at=now()`，写入 `run_events`（`run.failed`，error_class: `worker.timeout`）
    3. 按 org_id 分组，将 Redis counter `arkloop:org:active_runs:{org_id}` 重置为 `SELECT COUNT(*) FROM runs WHERE org_id=$1 AND status='running'`
  - 重置操作用 Redis WATCH + MULTI（乐观锁），避免与正常 INCR/DECR 冲突。
  - 审计：每次强制失败都写审计日志（actor: `system`）。
  - 5 分钟阈值可通过环境变量配置（`ARKLOOP_RUN_TIMEOUT_MINUTES`）。
- **具体改动范围**：
  - 新建 `src/services/api/internal/jobs/stale_run_reaper.go`：定时扫描 + 强制失败 + 计数器重置。
  - 修改 `src/services/api/cmd/api/main.go`：启动时 go routine 运行 reaper。
  - 修改 `src/services/api/internal/data/runs_repo.go`：新增 `ListStaleRunning` 和 `ForceFailRun` 方法。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 run，手动 kill Worker 进程，等待 5 分钟 + 60 秒后确认 run 被标记为 failed，Redis counter 已重置。

### R74 -- PostgreSQL 只读副本配置

- **关联审计**：九.9.4「没有 PostgreSQL 只读副本」
- **目标**：为读密集查询（审计列表、run_events 历史回放、threads/messages 列表）配置只读副本分流。
- **关键点**：
  - 生产环境使用 PostgreSQL streaming replication 创建只读副本。
  - 开发环境不配置副本（compose.yaml 不改动）。
  - 应用层新增 `ARKLOOP_DATABASE_READ_URL` 环境变量，指向只读副本。
  - API 服务持有两个连接池：`writePool`（主库）和 `readPool`（只读副本，可选）。
  - Repository 层对只读查询（List、Get）使用 `readPool`；写操作（Create、Update、Delete）使用 `writePool`。
  - `readPool` 未配置时 fallback 到 `writePool`（开发环境零影响）。
  - 注意事项：只读副本有复制延迟（通常 < 100ms），写后立即读的场景（如创建 run 后立即查询状态）必须走主库。
- **具体改动范围**：
  - 修改 `src/services/api/internal/data/`：Repository 方法区分读写连接池。
  - 修改 `src/services/api/cmd/api/main.go`：初始化双连接池。
  - `.env.example` 新增 `ARKLOOP_DATABASE_READ_URL`。
  - 文档：说明生产环境如何配置 streaming replication（不在代码仓库中实现，只写运维指引）。
- **验收**：
  - `go test -tags integration ./...`（不配置 READ_URL 时 fallback 正常）。
  - 手工（生产环境）：配置只读副本后，Console 审计查询走只读副本（通过 pg_stat_activity 确认）。

---

## 9.5 Phase 8 -- 公开发布基础（Launch Readiness）

目标：将平台从「内部 Demo」推进到「可公开推广并正常运营」的状态。Phase 6/6.5 完成了后端 CRUD 和 Console 管理面的骨架，但管理员看不到有意义的统计数据、没有病毒式推广手段、没有滥用防护机制、通知不能广播。Phase 8 补齐这些缺口，使产品具备最低限度的公开发布条件。

核心策略：免费 + 邀请制 + 积分消耗。每位用户通过邀请码注册后获得初始积分（如 1000），积分按 token 消耗扣减，邀请他人可获得额外积分奖励。Subscription 和正式计费后置。

前置依赖：Phase 6（企业级能力，特别是 R62.1 Plans/Entitlements、R62.2 Usage Metering、R64 通知系统）+ Phase 6.5（Console 基础组件和页面框架）。

### R80 -- Admin Dashboard API（统计聚合端点）

- **目标**：提供 `GET /v1/admin/dashboard` 端点，返回平台级关键指标，让管理员一眼看到系统现状。
- **关键点**：
  - 返回字段（最小集）：`total_users`、`active_users_30d`、`total_runs`、`runs_today`、`total_input_tokens`、`total_output_tokens`、`total_cost_usd`、`active_orgs`。
  - 聚合查询走只读路径（如果 R74 已部署则走 readPool）。
  - 重查询加简单缓存（Redis TTL 60s 或进程内 TTL），避免每次请求扫全表。
  - 权限：仅 `platform.admin`。
- **具体改动范围**：
  - `src/services/api/internal/data/users_repo.go`：新增 `CountAll(ctx)`、`CountActiveSince(ctx, since time.Time)`。
  - `src/services/api/internal/data/runs_repo.go`：新增 `CountAll(ctx)`、`CountSince(ctx, since time.Time)`。
  - `src/services/api/internal/data/usage_repo.go`：新增 `GetGlobalSummary(ctx)` 或在现有 `GetMonthlyUsage` 基础上增加全局版本。
  - `src/services/api/internal/data/orgs_repo.go`：新增 `CountActive(ctx)`。
  - 新建 `src/services/api/internal/http/v1_admin_dashboard.go`：聚合上述 repo 调用。
  - `src/services/api/internal/http/handler.go`：注册 `GET /v1/admin/dashboard`。
- **依赖**：R66（全局 Runs List 端点已存在）、R60（权限点）
- **验收**：
  - integration test：`GET /v1/admin/dashboard` 返回所有字段且类型正确。
  - integration test：非 platform.admin 被拒绝。

### R80.5 -- /v1/me 增强 + 用户自助查询（解决"不知道自己 org_id"的问题）

- **目标**：让当前登录用户能通过 `/v1/me` 获取完整的上下文信息（包括 org_id、角色、积分余额），并提供不需要手动传 org_id 的自助查询端点。
- **问题背景**：当前 `/v1/me` 只返回 `id`、`display_name`、`created_at`、`permissions`，不返回 `org_id`。而 `/v1/orgs/{id}/usage` 等端点要求传入 org_id，导致前端和管理员根本不知道该填什么。这是一个严重的可用性缺陷。
- **关键点**：
  - **`GET /v1/me` 响应增强**：新增 `org_id`、`org_name`、`role` 字段（从 `org_memberships` 的 default membership 获取）。
  - **`GET /v1/me/usage`**：自动使用当前用户的 org_id 查询用量，不需要手动传 org_id。支持 `?year=&month=` 参数。
  - **`GET /v1/me/credits`**：自动使用当前用户的 org_id 查询积分余额和最近交易。
  - **`GET /v1/me/invite-code`**：获取当前用户的邀请码。
  - 这些 `/v1/me/*` 端点是对应 admin 端点的用户自助版本，只返回自己 org 的数据，不需要任何 ID 参数。
- **具体改动范围**：
  - `src/services/api/internal/http/v1_auth.go`：`meResponse` struct 新增 `OrgID`、`OrgName`、`Role`；`me` handler 从 membership 读取并填充。
  - 新建 `src/services/api/internal/http/v1_me.go`：`/v1/me/usage`、`/v1/me/credits`、`/v1/me/invite-code` 端点（内部自动获取 actor.OrgID，不需要 path param）。
  - `src/services/api/internal/http/handler.go`：注册新路由。
- **依赖**：无前置依赖（可最先做，因为只扩展已有能力）
- **验收**：
  - integration test：`GET /v1/me` 返回有效的 `org_id` 和 `role`。
  - integration test：`GET /v1/me/usage` 返回当前 org 的用量数据，无需手动传 org_id。

### R81 -- User Management API（列表 / 搜索 / 封禁）

- **目标**：让管理员能在 Console 中查看、搜索和管理用户。当前 `UserRepository` 只有 `Create`/`GetByID`/`BumpTokensInvalidBefore`，无法列表。
- **关键点**：
  - `GET /v1/admin/users`：分页列表（cursor-based），支持 `?q=` 模糊搜索（display_name / email）、`?status=` 过滤。
  - `GET /v1/admin/users/{user_id}`：详情（含 org 归属、最后登录时间、注册来源）。
  - `PATCH /v1/admin/users/{user_id}`：更新 status（active / suspended），封禁用户。封禁操作必须写审计。
  - 搜索走 `ILIKE` 或 `pg_trgm`，v1 用 ILIKE 足够。
  - 权限：仅 `platform.admin`。
- **具体改动范围**：
  - `src/services/api/internal/data/users_repo.go`：新增 `List(ctx, cursor, limit, query, status)`、`UpdateStatus(ctx, userID, status)`。
  - 新建 `src/services/api/internal/http/v1_admin_users.go`。
  - `src/services/api/internal/http/handler.go`：注册路由。
- **依赖**：R60（权限点）
- **验收**：
  - integration test：创建多个用户后 `GET /v1/admin/users` 分页正确；搜索可命中；封禁后该用户登录被拒绝。

### R82 -- 邀请码系统（个人邀请码 + 推荐链追踪 + 限额控制）

- **目标**：每个用户拥有唯一邀请码，新用户注册时可填写邀请码，建立推荐关系，形成放射状推广网络。通过限额机制防止滥用。
- **关键点**：
  - 邀请码格式：8 位大写字母 + 数字，用户创建时自动生成，也可手动重置（限频）。
  - `invite_codes` 表：`id, user_id, code(unique), max_uses, use_count, is_active, created_at`。**`max_uses` 不可为空**，默认值由平台设置控制。
  - `referrals` 表：`id, inviter_user_id, invitee_user_id, invite_code_id, credited, created_at`。
  - **邀请限额机制**（核心防滥用手段）：
    - 平台全局设置（entitlement key）：
      - `invite.max_codes_per_user`：每个用户最多同时拥有几个有效邀请码（默认 1）。
      - `invite.default_max_uses`：每个邀请码默认可邀请人数（默认 1）。
    - 管理员可对单个用户或单个邀请码手动调整 `max_uses`。
    - 用户尝试生成新邀请码时，如果已有有效码数量达到 `invite.max_codes_per_user` 上限，拒绝并返回稳定错误码。
    - 邀请码 `use_count >= max_uses` 时自动标记为不可用，但不删除（保留审计链）。
  - 推荐链查询：`GET /v1/admin/referrals?inviter_user_id=` 查看某用户的下线；`GET /v1/admin/referrals/tree?user_id=` 查看推广树（限深度 3 层，防 N+1）。
  - 用户端：`GET /v1/me/invite-code` 获取自己的邀请码；`POST /v1/me/invite-code/reset` 重置（每 24 小时限一次）。
  - 管理端：`GET /v1/admin/invite-codes` 列表/搜索；`PATCH /v1/admin/invite-codes/{id}`（停用 / 调整 max_uses）；`GET /v1/admin/invite-settings`（查看全局限额配置）；`PATCH /v1/admin/invite-settings`（修改全局限额）。
  - 邀请码与 org invitation 是两套独立系统：org invitation 是组织内协作邀请，invite code 是平台级增长工具。
- **具体改动范围**：
  - goose migration：新建 `invite_codes` 和 `referrals` 表。
  - 新建 `src/services/api/internal/data/invite_codes_repo.go`。
  - 新建 `src/services/api/internal/data/referrals_repo.go`。
  - 新建 `src/services/api/internal/http/v1_invite_codes.go`（用户端 + 管理端）。
  - `src/services/api/internal/http/handler.go`：注册路由。
  - 全局限额通过 entitlement key 存储（复用 R62.1 的 plan_entitlements / org_entitlement_overrides），不新建设置表。
- **依赖**：R81（用户管理，封禁用户时其邀请码应自动停用）、R62.1（Entitlements，用于读取全局限额配置）
- **验收**：
  - integration test：用户注册后自动获得邀请码（max_uses = 默认值）。
  - integration test：邀请码 use_count 达到 max_uses 后无法再使用。
  - integration test：用户有效码数量达到上限后无法再生成新码。
  - integration test：管理员调整 max_uses 后立即生效。
  - integration test：管理端可查看推荐链。

### R83 -- 兑换码系统（一次性激活码 / 批量生成）

- **目标**：管理员可批量生成兑换码，用户输入兑换码后获得积分或激活特定功能。用于运营活动、合作推广、内测激活等场景。
- **关键点**：
  - `redemption_codes` 表：`id, code(unique), type(credit/feature), value(积分数量或 feature key), max_uses, use_count, expires_at, batch_id, created_by_user_id, created_at`。
  - `redemption_records` 表：`id, code_id, user_id, org_id, redeemed_at`。
  - 管理端：`POST /v1/admin/redemption-codes/batch`（批量生成，指定数量/类型/面值/有效期）；`GET /v1/admin/redemption-codes`（列表/搜索）；`PATCH /v1/admin/redemption-codes/{id}`（停用）。
  - 用户端：`POST /v1/me/redeem`（兑换，输入 code）。
  - 兑换码格式：16 位大写字母 + 数字，分 4 组用短横线分隔（如 `ABCD-EF12-GH34-JK56`）。
  - 同一用户同一码只能兑换一次；同一批次的码可设总使用次数。
  - 审计：兑换操作必须写审计日志。
- **具体改动范围**：
  - goose migration：新建 `redemption_codes` 和 `redemption_records` 表。
  - 新建 `src/services/api/internal/data/redemption_codes_repo.go`。
  - 新建 `src/services/api/internal/http/v1_redemption_codes.go`。
  - `src/services/api/internal/http/handler.go`：注册路由。
- **依赖**：R84（兑换积分时需要积分系统）
- **验收**：
  - integration test：批量生成 10 个码 -> 用户兑换 -> 余额增加 -> 同码重复兑换被拒绝。
  - integration test：过期码兑换被拒绝。

### R84 -- 积分体系（Credit System）

- **目标**：建立基于积分的资源消耗控制机制。积分是 Arkloop 的内部通货，邀请新用户、兑换码、管理员充值都可增加积分。
- **兑换比例**：**1 积分 = 1,000 Token**（即 1K token 消耗 1 积分）。实际扣减按模型倍率折算。
- **关键点**：
  - `credits` 表（org 级别）：`id, org_id, balance(bigint), updated_at`。积分挂在 org 下而非 user，与多租户模型一致。
  - `credit_transactions` 表：`id, org_id, amount(可正可负), type(invite_reward/redemption/consumption/admin_adjustment/initial_grant), reference_type(nullable), reference_id(nullable), note(nullable), created_at`。
  - 初始积分：新 org 创建时自动发放（由 entitlement `credit.initial_grant` 控制，默认 1000）。
  - 邀请奖励：被邀请用户完成注册后，邀请人的 org 获得奖励积分（由 entitlement `credit.invite_reward` 控制，默认 500）。
  - **模型倍率与成本配置**（在 `llm_routes` 表新增字段）：
    - `multiplier` (float64, default 1.0)：积分消耗倍率。例如 GPT-4o 倍率 2.0 表示消耗 1K token 扣 2 积分。
    - `cost_per_1k_input` (float64, nullable)：平台方采购该模型 1K input token 的实际成本（USD），用于后台成本统计。
    - `cost_per_1k_output` (float64, nullable)：平台方 1K output token 的实际成本（USD）。
    - 这些字段在创建/编辑 LLM 凭证路由时由管理员配置。
  - **积分扣减公式**：`ceil((input_tokens + output_tokens) / 1000 * multiplier)`，向上取整，最小扣 1 积分。
  - **成本统计公式**（仅后台统计，不面向用户）：`input_tokens / 1000 * cost_per_1k_input + output_tokens / 1000 * cost_per_1k_output`，结果写入 `usage_records.cost_usd`。
  - 消耗：Worker 执行 run 完成后，根据 `usage_records` 中的 token 数和路由的 `multiplier` 扣减积分。积分不足时 run 被拒绝（在 pipeline 的 entitlement middleware 中检查）。
  - 管理端：`GET /v1/admin/credits?org_id=`（查看 org 积分余额 + 交易记录）；`POST /v1/admin/credits/adjust`（管理员手动充值/扣减，必须填 note）。
  - 用户端：`GET /v1/me/credits`（查看当前 org 的余额和最近交易）。
  - 并发安全：余额扣减使用 `UPDATE credits SET balance = balance - $1 WHERE org_id = $2 AND balance >= $1`（行锁 + 余额检查原子操作）。
- **具体改动范围**：
  - goose migration：
    - `llm_routes` 表新增 `multiplier`（default 1.0）、`cost_per_1k_input`（nullable）、`cost_per_1k_output`（nullable）列。
    - 新建 `credits` 和 `credit_transactions` 表。
  - `src/services/api/internal/data/llm_routes_repo.go`：`LlmRoute` struct 新增三个字段；`Create` 和相关方法适配。
  - `src/services/api/internal/http/v1_llm_credentials.go`：创建凭证时的路由请求体新增 `multiplier` / `cost_per_1k_input` / `cost_per_1k_output`。
  - 新建 `src/services/api/internal/data/credits_repo.go`。
  - 新建 `src/services/api/internal/http/v1_credits.go`。
  - 修改 `src/services/worker/internal/routing/config.go`：`ProviderRouteRule` 新增 `Multiplier`、`CostPer1kInput`、`CostPer1kOutput`；`LoadRoutingConfigFromDB` 读取新字段。
  - 修改 `src/services/worker/internal/pipeline/mw_entitlement.go`：在 run 开始前检查积分余额。
  - 修改 Worker usage 记录写入逻辑：run 完成后按公式扣减积分、写入实际成本。
  - `src/services/api/internal/http/handler.go`：注册路由。
- **依赖**：R62.1（Entitlements Service，用于读取积分配置项）、R62.2（Usage Metering，用于计算 token 消耗）
- **验收**：
  - integration test：新 org 创建后 balance = initial_grant。
  - integration test：run 执行后 balance 扣减正确（`ceil((input + output) / 1000 * multiplier)`）。
  - integration test：倍率 2.0 的模型消耗 1500 token 扣减 3 积分（`ceil(1.5 * 2.0) = 3`）。
  - integration test：余额不足时 run 被拒绝且返回稳定错误码。
  - integration test：`usage_records.cost_usd` 按 `cost_per_1k_input/output` 正确计算。
  - integration test：管理员调整余额后交易记录正确。

### R85 -- Usage 跟踪增强 + Console Usage 页面

- **目标**：将当前仅有月度汇总的 usage 查询扩展为多维度可视化数据源，并实现 Console 的 Usage 页面。
- **关键点**：
  - 后端新增查询：
    - `GetDailyUsage(ctx, orgID, startDate, endDate)`：按日聚合。
    - `GetUsageByModel(ctx, orgID, year, month)`：按模型分组。
    - `GetGlobalDailyUsage(ctx, startDate, endDate)`：平台级日用量（admin 用）。
  - Console Usage 页面（替换当前 Placeholder）：
    - 日用量折线图（最近 30 天）。
    - 按模型的 token 消耗饼图。
    - 积分余额卡片 + 最近交易记录列表。
  - 图表库：使用轻量方案（如 CSS 绘制或 `recharts`，优先选择项目已有的库）。
- **具体改动范围**：
  - `src/services/api/internal/data/usage_repo.go`：新增上述查询方法。
  - `src/services/api/internal/http/v1_usage.go`：扩展端点或新增子路由。
  - `src/apps/console/src/pages/usage/`：替换 Placeholder，实现完整 Usage 页面。
  - `src/apps/console/src/api/usage.ts`：新增 API client。
- **依赖**：R62.2（usage_records 表已存在）、R84（积分数据）
- **验收**：
  - integration test：写入多条 usage_records 后日聚合、模型分组查询正确。
  - 手工：Console Usage 页面图表正确渲染。

### R86 -- 通知广播（系统公告 + 批量发送）

- **目标**：让管理员能在 Console 中发布系统公告（如服务器维修通知），并推送到所有用户或指定 org 的用户。
- **关键点**：
  - 后端新增：
    - `POST /v1/admin/notifications/broadcast`：参数 `{ type, title, body, target: "all" | org_id, payload_json }`。
    - 广播实现：后台异步写入（按 org 分批，每批 INSERT ... SELECT 从 org_memberships 展开到 notifications 表）。不阻塞 API 响应。
    - 广播任务投递到 jobs 表，由 Worker 或 API 后台 goroutine 消费执行。
  - `notifications` 表新增可选字段 `broadcast_id`（关联同一次广播的所有通知，便于撤回和统计）。
  - 管理端：`GET /v1/admin/notifications/broadcasts`（广播历史列表，含送达数/已读数）。
  - Console 通知管理页面增加「发布公告」功能。
  - 不在此步实现 WebSocket/SSE 实时推送（v2 再做），前端轮询 `/v1/notifications` 即可。
- **具体改动范围**：
  - goose migration：`notifications` 表新增 `broadcast_id` 列。新建 `notification_broadcasts` 表（id, type, title, body, target_type, target_id, payload_json, sent_count, created_by_user_id, created_at）。
  - `src/services/api/internal/data/notifications_repo.go`：新增 `BroadcastToAll`、`BroadcastToOrg`、`ListBroadcasts`。
  - 新建 `src/services/api/internal/http/v1_admin_notifications.go`。
  - `src/services/api/internal/http/handler.go`：注册路由。
  - Console：通知管理页面增加「发布公告」表单。
- **依赖**：R64（通知系统基础已存在）
- **验收**：
  - integration test：广播到 all -> 所有用户的 unread 通知中出现该条目。
  - integration test：广播到指定 org -> 仅该 org 用户收到。

### R87 -- 注册流程集成（注册模式控制 + 邀请码校验 + 积分发放）

- **目标**：实现可切换的注册模式（邀请制 / 开放注册），并将邀请码和积分发放集成到注册流程中。
- **关键点**：
  - **注册模式控制**（通过已有的 Feature Flag 基础设施实现，不新建表）：
    - feature flag：`registration.open`，`default_value = false`（默认邀请制）。
    - 新增公开端点 `GET /v1/auth/registration-mode`（不需要鉴权），返回 `{ "mode": "invite_only" | "open" }`。内部读取 `feature_flags` 表的 `default_value`。
    - `FeatureFlagService` 新增 `IsGloballyEnabled(ctx, flagKey)` 方法：只查全局 `default_value`，不需要 org_id（注册场景无 org 上下文）。
    - 管理员在 Console Feature Flags 页面切换 `registration.open` 即可在邀请制和开放注册之间切换，无需改代码或重启。
  - **注册行为**：
    - `POST /v1/auth/register` 增加可选字段 `invite_code`。
    - 邀请制模式（`registration.open = false`）：`invite_code` 必填，为空直接拒绝（返回 `auth.invite_code_required`）。
    - 开放注册模式（`registration.open = true`）：`invite_code` 选填，提供则建立推荐关系。
  - 注册事务内的操作序列：
    1. 检查注册模式，邀请制下校验 invite_code 有效性。
    2. 创建 user + 默认 org + org_membership（已有逻辑）。
    3. 为新 org 初始化积分余额（credit.initial_grant）。
    4. 为新用户生成邀请码。
    5. 如果提供了有效 invite_code：写入 referrals -> 给邀请人 org 发放奖励积分。
  - 开放注册模式下，无效邀请码不阻止注册（仅跳过推荐关系建立），但返回 warning 字段提示。
  - **Web 前端 AuthPage 适配**：
    - 页面加载时调 `GET /v1/auth/registration-mode` 获取当前模式。
    - 邀请制：注册表单新增"邀请码"输入框（必填）。
    - 开放注册：邀请码输入框选填或隐藏。
  - 审计：注册 + 推荐关系 + 积分发放 + 模式切换全部写审计。
- **具体改动范围**：
  - `src/services/api/internal/featureflag/service.go`：新增 `IsGloballyEnabled` 方法。
  - `src/services/api/internal/http/v1_auth.go`：`registerRequest` 增加 `InviteCode` 字段；`register` handler 增加模式检查；新增 `GET /v1/auth/registration-mode` handler。
  - `src/services/api/internal/auth/registration.go`：扩展注册逻辑。
  - `src/apps/web/src/components/AuthPage.tsx`：加载注册模式，条件性显示邀请码输入框。
  - `src/apps/web/src/api/`：新增 `getRegistrationMode` API client。
- **依赖**：R82（邀请码系统）、R84（积分体系）、R63（Feature Flags，已完成）
- **验收**：
  - integration test：`registration.open = false` 时不带邀请码注册被拒绝（`auth.invite_code_required`）。
  - integration test：`registration.open = false` 时带有效邀请码注册成功。
  - integration test：`registration.open = true` 时不带邀请码注册成功。
  - integration test：带有效邀请码注册 -> referrals 记录存在 -> 邀请人 org 余额增加。
  - integration test：切换 flag 后行为立即改变。
  - integration test：新用户 org 初始积分余额正确。
  - 手工：Web AuthPage 在两种模式下表现正确。

### R88 -- Console Phase 8 页面

- **目标**：为 Phase 8 新增的后端功能提供 Console 管理界面。
- **关键点**：
  - **Dashboard 页面**（替换当前 `<Navigate to="/runs" />`）：
    - 顶部统计卡片：用户总数、今日活跃、今日 Runs、积分总消耗。
    - 最近注册用户列表（最近 10 条）。
    - 数据来源：`GET /v1/admin/dashboard`。
  - **Users 管理页面**（新增路由 `/users`）：
    - 用户列表 + 搜索 + 状态过滤。
    - 用户详情弹窗（基本信息、所属 org、积分余额、推荐链）。
    - 封禁/解封操作。
  - **Invite Codes 管理页面**（新增路由 `/invite-codes`）：
    - 邀请码列表 + 搜索。
    - 停用操作。
    - 推荐链可视化（简单树形列表，限 3 层）。
  - **Redemption Codes 管理页面**（新增路由 `/redemption-codes`）：
    - 兑换码列表 + 搜索 + 批量生成表单。
    - 兑换记录列表。
  - **Credits 管理页面**（新增路由 `/credits`，或集成到 Usage 页面）：
    - 各 org 积分余额排行。
    - 管理员调整余额表单。
    - 交易记录列表。
  - 侧边栏导航需新增对应菜单项。
- **具体改动范围**：
  - 新建 `src/apps/console/src/pages/dashboard/DashboardPage.tsx`。
  - 新建 `src/apps/console/src/pages/users/UsersPage.tsx`。
  - 新建 `src/apps/console/src/pages/invite-codes/InviteCodesPage.tsx`。
  - 新建 `src/apps/console/src/pages/redemption-codes/RedemptionCodesPage.tsx`。
  - 新建对应的 `src/apps/console/src/api/` client 文件。
  - 修改 `src/apps/console/src/App.tsx`：新增路由，首页改为 Dashboard。
  - 修改 `src/apps/console/src/layouts/ConsoleLayout.tsx`：侧边栏新增菜单项。
  - 修改 `src/apps/console/src/locales/`：新增翻译条目。
- **依赖**：R80-R87（后端 API 全部就绪）
- **验收**：
  - 手工：Dashboard 页面显示正确的统计数据。
  - 手工：Users 页面可搜索、可封禁。
  - 手工：Invite Codes 页面可查看推荐链。
  - 手工：Redemption Codes 页面可批量生成、可查看兑换记录。
  - `pnpm build` 无报错。

---

## 10. run_events 分区策略（跨 Phase 长期任务）

- **关联审计**：二.2.5「无分区无归档」；P0
- **关联目标架构**：六.6.7 `run_events_v2` 按月分区

run_events 的分区改造在 Phase 7 的 R71 中正式落地。原始策略保留供参考：

1. **Phase 1（R13）**：已完成，消除行锁热点（sequence）。
2. **Phase 7（R71）**：执行月分区改造。详见 8.5 节 R71。
3. **分区方案**：见 R71。
4. **成本字段抽列**：在 Phase 6（R62.2）的 `usage_records` 表中独立存储 token 用量和费用。

---

## 11. 执行顺序与依赖关系

```
Phase 1（地基）
  R10 → R11 → R12 → R13 → R14 → R15 → R16
  （全部是 migration + repository 层，互相无强依赖，可并行执行）

Phase 2（基础设施）
  R20 → R21
  （compose.yaml + 连通性验证）

Phase 3（Worker 重构）
  R30 → R31 → R32 → R33 → R34 → R34.5 → R35 → R36
  （R32 先行，R33/R34/R35 依赖 R32；R36 依赖 R20）

Phase 4（Gateway + 安全）
  R40 → R41 → R42 → R43 → R44
  （R40 先行，R41/R42 可并行，R43 在 R40 之后）

Phase 4.5（Worker 执行 Pipeline）
  R45 → R46
  （R45 定义框架，R46 依赖 R45 做实际重构；必须在 Phase 5 之前完成）

Phase 5（组织与权限）
  R50 → R51 → R52
  （R50/R51 可并行，R52 在 R15 之后）

Phase 6（企业级能力）
  R60 → R61 → R62.1 ──→ R62.3
                R62.2 ──↗
              R63 → R64
  （R61 依赖 R52；R62.1 和 R62.2 可并行，R62.3 依赖 R62.1 + R62.2；R63/R64 与 R62 链无强依赖，可并行）

Phase 6.5（Console Management）
  R65 + R66（后端补全，可并行）
  → R67.1（共享组件 + 导航重构）
  → Phase 6.5a: R67.2 + R67.3 + R67.4（运营核心面，可并行）
  → Phase 6.5b: R67.5 + R67.6 + R67.7 + R67.8 + R67.9（配置管理面，可并行）
  → Phase 6.5c: R68.1 ~ R68.11（集成/安全/组织/计费/平台，可并行）
  （R67.2 依赖 R66；R67.4 依赖 R65；其余页面只依赖 R67.1 共享组件）
```

Phase 7（性能与可扩展性）
  R70 → R71 → R72 → R73 → R74
  （R70 先行解锁扩展上限，R71/R72 可并行，R73 依赖 R72，R74 运维层面独立）

Phase 8（公开发布基础）
  R80.5（/v1/me 增强，无前置依赖，最先做）
  → R80 + R81（Dashboard + 用户管理，可并行）
  → R82（邀请码系统 + 限额控制）
  → R84（积分体系 + 模型倍率/成本，依赖 R62.1 + R62.2）
  → R83（兑换码系统，依赖 R84）
  → R85（Usage 增强，依赖 R84）
  → R86（通知广播，依赖 R64）
  → R87（注册流程集成，依赖 R82 + R84）
  → R88（Console 页面，依赖 R80-R87 全部就绪）
  （R80/R81 可与 R82 并行；R86 与 R82-R85 无强依赖可并行）
```

Phase 1-4 是核心路径，Phase 4.5 是 Phase 5 的前置条件。Phase 5-6 可根据产品优先级调整顺序。Phase 6.5 在 Phase 6 功能验收完成后执行，与 Phase 7 可并行推进（Console 开发不阻塞性能优化）。Phase 8 是公开发布的前置条件，优先级高于 Phase 7 -- 可以在 Phase 6.5 完成后直接推进 Phase 8，Phase 7 的性能优化并行或后置。同一 Phase 内无强依赖的薄片可并行执行。

---

## 12. 每个 Phase 的退出标准

| Phase | 退出标准 |
|---|---|
| Phase 1 | 全部 migration 通过 up/down；`go test -tags integration ./...` 全绿；现有 Web 功能不退化 |
| Phase 2 | `docker compose up -d` 三个服务（postgres/redis/minio）全部 healthy；连通性测试全绿 |
| Phase 3 | Worker 使用 DB 凭证/MCP 配置/Skills 执行 run；SSE 延迟 < 100ms；runs.status 可信 |
| Phase 4 | 所有流量经过 Gateway；限流生效；API Key 可创建/使用/吊销；IP 过滤生效 |
| Phase 4.5 | `go test -tags integration ./...`（worker）全绿且无测试修改；`v1.go` Execute 方法不超过 30 行；Pipeline 框架单测全绿 |
| Phase 5 | org 邀请流程完整；RBAC 权限点校验生效；projects 层级可用 |
| Phase 6 | Webhooks 投递可用；agent_configs 继承链生效；Plans/Subscriptions/Entitlements CRUD 可用（R62.1）；usage_records 自动写入（R62.2）；配额执行检查生效（R62.3） |
| Phase 6.5a | 共享组件可用；Runs/Notifications/Audit 三个页面 CRUD 功能完整；导航分组正确；`pnpm build` 无报错 |
| Phase 6.5b | LLM Credentials/Agent Configs/Prompt Templates/MCP Configs/Skills 五个页面 CRUD 功能完整 |
| Phase 6.5c | API Keys/Webhooks/IP Rules/Teams/Projects/Members/Plans/Subscriptions/Entitlements/Usage/Feature Flags 全部页面 CRUD 功能完整；全部 16+ 后端资源可通过 Console 管理 |
| Phase 7 | PgBouncer 部署；run_events 月分区；SSE 多实例广播；50 Worker + 1000 并发 Run 可稳定运行 |
| Phase 8 | Dashboard 显示真实统计；用户列表可搜索可封禁；邀请码注册流程完整；兑换码可批量生成可兑换；积分余额正确扣减；余额不足时 run 被拒绝；通知广播可送达全部用户；`pnpm build` 无报错 |

---

## 13. 风险与缓解

| 风险 | 缓解措施 |
|---|---|
| migration 导致数据丢失 | 全部 migration 提供 down 回滚脚本；上线前备份数据库 |
| run_events sequence 全局递增破坏前端 seq 连续性假设 | 确认前端只做 `> after_seq` 比较，不假设连续（已验证当前代码不依赖连续） |
| Redis 引入增加运维复杂度 | 开发环境用 compose 管理；生产环境前期可用 managed Redis |
| Gateway 引入后 SSE 长连接透传 | Gateway 用 `httputil.ReverseProxy` 自带的 Flush 支持；专门测试 SSE 透传场景 |
| MCP 远端化后延迟增加 | stdio fallback 保留；Remote MCP 走 HTTP keep-alive + 工具列表缓存到 Redis |
| 多个 migration 版本管理 | goose 的版本号严格递增；每个 Phase 完成后做一次 `ExpectedVersion` 对齐 |
| PgBouncer transaction mode 下 LISTEN 不可用 | SSE handler 的 LISTEN 连接走直连 DSN，不经过 PgBouncer |
| run_events 分区迁移期间数据不一致 | 双写 + 游标去重，切换前充分验证 |
| Worker 崩溃计数器泄漏在修复窗口内（5min）阻塞 org | 阈值可配置；紧急情况可手动 `redis-cli SET arkloop:org:active_runs:{org_id} 0` |
| 只读副本复制延迟导致写后读不一致 | 写后立即读的场景（如创建 run 后查状态）强制走主库 |
| Pipeline 重构引入行为回归 | 全量 integration test 不修改地通过作为铁律；重构期间不加新功能，纯结构变更 |
| RunContext 字段膨胀导致 God Object | RunContext 只承载 per-run 可变状态，不持有服务依赖；Middleware 的服务依赖通过闭包注入 |
| Console 共享组件不够用导致每页面重复造轮子 | 在 R67.1 中充分覆盖 DataTable/Modal/FormField/Badge/Toast 五类核心组件；6.5a 三个页面验证组件是否充分，不足时在 6.5b 开始前补充 |
| 16+ 页面的 API client 代码膨胀 | 统一使用 `apiFetch` 基础，每个资源域独立文件，不做过度抽象（不搞 auto-generate）；页面间不共享业务状态 |
| 积分并发扣减导致超扣 | `UPDATE credits SET balance = balance - $1 WHERE org_id = $2 AND balance >= $1` 原子操作保证不超扣；高并发场景下行锁是可接受的瓶颈 |
| 邀请码滥用（批量注册刷积分） | 邀请码有 max_uses 上限；注册频率限制在 Gateway 限流层；管理员可随时停用邀请码 |
| 广播通知写入量大（10 万用户 = 10 万行 INSERT） | 异步执行，按 org 分批 INSERT ... SELECT；不阻塞 API 响应；大规模场景下可改为 COPY |
| Dashboard 聚合查询慢 | 加 Redis TTL 缓存（60s）；走只读副本（R74）；后期可用物化视图 |
